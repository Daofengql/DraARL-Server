package udphub

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/models"
	"draarl/internal/protocol"
	drivermysql "github.com/go-sql-driver/mysql"
)

func TestDeviceCapabilitySwitchPreservesConfigurationMySQL(t *testing.T) {
	if os.Getenv("DRAARL_DEVICE_CONFIG_E2E") != "true" {
		t.Skip("requires disposable MySQL")
	}
	parsed, err := drivermysql.ParseDSN(os.Getenv("DRAARL_TEST_MYSQL_DSN"))
	if err != nil || parsed == nil || !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatal("disposable draarl_test_ database required")
	}
	parsed.ParseTime = true
	if err := gormdb.Init(&gormdb.Config{DSN: parsed.FormatDSN(), MaxOpenConns: 5, MaxIdleConns: 2, LogLevel: "silent"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gormdb.Close() })
	if err := gormdb.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	db := gormdb.Get()
	owner := &gormdb.User{Name: "capabilities", Email: "capabilities@example.invalid", CallSign: "CAPS", Status: 1}
	if err := db.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	row := &gormdb.Device{Name: "capabilities-1", OwnerID: owner.ID, SSID: 1, DevModel: 1, Status: 1}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	repo := gormdb.NewDeviceConfigRepository()
	t.Cleanup(func() {
		_ = repo.DeleteDeviceConfigs(row.ID)
		_ = db.Delete(row).Error
		_ = db.Delete(owner).Error
	})
	dev := &models.Device{ID: row.ID, OwnerID: owner.ID, SSID: 1, Username: owner.Name, DevModel: 1}
	runtimeIndexMu.Lock()
	previous := devOwnerSSIDMap
	devOwnerSSIDMap = map[string]*models.Device{getOwnerSSIDKey(owner.ID, 1): dev}
	runtimeIndexMu.Unlock()
	t.Cleanup(func() { runtimeIndexMu.Lock(); devOwnerSSIDMap = previous; runtimeIndexMu.Unlock() })
	if err := repo.SetDeviceConfigs(row.ID, map[string]string{"rx_freq": "438500000", "adc_volume": "75", "rf_guard_enabled": "1"}); err != nil {
		t.Fatal(err)
	}
	if !applyClientReportedDevModel(context.Background(), dev, 2) {
		t.Fatal("RF to network switch was not accepted")
	}
	configs, err := GetDeviceConfigsFromDB(row.ID)
	if err != nil || len(configs) != 1 || configs["adc_volume"] != "75" {
		t.Fatalf("network API exposed inactive configuration: %v %v", configs, err)
	}
	if _, err := SaveDeviceConfigReportAndBuildAck(row.ID, buildConfigSetPacket(map[string]string{"adc_volume": "85", "sql_active_high": "1", "rx_freq": "439500000"})); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetDeviceConfigs(row.ID)
	if err != nil || stored["rx_freq"] != "438500000" || stored["adc_volume"] != "85" {
		t.Fatalf("network report overwrote RF settings: %v %v", stored, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if applyClientReportedDevModel(ctx, dev, 1) || dev.RuntimeSnapshot().DevModel != 2 {
		t.Fatal("failed database update changed runtime model")
	}
	if !applyClientReportedDevModel(context.Background(), dev, 1) {
		t.Fatal("network to RF retry was not accepted")
	}
	configs, err = GetDeviceConfigsFromDB(row.ID)
	if err != nil || configs["rx_freq"] != "438500000" || configs["adc_volume"] != "85" || configs["sql_active_high"] != "" || configs["rf_guard_enabled"] != "" {
		t.Fatalf("RF configuration not restored correctly: %v %v", configs, err)
	}
	// Actual UDP output must contain saved RF values, query missing new capabilities,
	// and finish with time sync. Inactive IO and obsolete guard values never travel.
	receiver, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	sender, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	previousConn := globalConn
	globalConn = sender
	t.Cleanup(func() { globalConn = previousConn })
	dev.UpdateRuntime(func(current *models.Device) { current.UDPAddr = receiver.LocalAddr().(*net.UDPAddr) })
	SyncDeviceConfig(dev)
	wantTypes := []byte{ConfigTypeSet, ConfigTypeQuery, ConfigTypeTimeSync}
	for _, want := range wantTypes {
		_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
		buffer := make([]byte, 4096)
		n, _, err := receiver.ReadFromUDP(buffer)
		if err != nil {
			t.Fatal(err)
		}
		data := buffer[protocol.DraARLv1HeaderSize:n]
		if len(data) == 0 || data[0] != want {
			t.Fatalf("unexpected config output %v, want %d", data, want)
		}
		if want == ConfigTypeSet {
			values := decodeTLV(data[2:])
			if values["rx_freq"] != "438500000" || len(values) != 2 {
				t.Fatalf("incorrect UDP configuration: %v", values)
			}
		}
	}
}
