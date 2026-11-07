package gormdb

import (
	"database/sql/driver"
	"strconv"
	"strings"
	"testing"

	"draarl/internal/models"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type deviceGroupStatement struct {
	sql      string
	group    driver.Value
	hasGroup bool
}

// Record the MySQL statements and driver values without connecting to a server.
func deviceGroupDryRunDB(t *testing.T) (*gorm.DB, *[]deviceGroupStatement) {
	t.Helper()
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{SkipInitializeWithVersion: true}), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	statements := make([]deviceGroupStatement, 0)
	capture := func(tx *gorm.DB) {
		statement := deviceGroupStatement{sql: tx.Statement.SQL.String()}
		var value interface{}
		if values, ok := tx.Statement.Clauses["VALUES"].Expression.(clause.Values); ok && len(values.Values) > 0 {
			for i, column := range values.Columns {
				if column.Name == "group_id" {
					value, statement.hasGroup = values.Values[0][i], true
				}
			}
		}
		// GORM removes the SET clause after building an UPDATE statement.
		if index := strings.Index(statement.sql, "`group_id`=?"); index >= 0 {
			parameter := strings.Count(statement.sql[:index], "?")
			value, statement.hasGroup = tx.Statement.Vars[parameter], true
		}
		if statement.hasGroup {
			statement.group, err = driver.DefaultParameterConverter.ConvertValue(value)
			if err != nil {
				t.Fatalf("convert group_id parameter: %v", err)
			}
		}
		statements = append(statements, statement)
	}
	for _, err := range []error{
		db.Callback().Create().After("gorm:create").Register("test:device_group", capture),
		db.Callback().Update().After("gorm:update").Register("test:device_group", capture),
		db.Callback().Query().After("gorm:query").Register("test:device_group", capture),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return db, &statements
}

func TestDeviceGroupPersistenceUsesNullableMySQLValue(t *testing.T) {
	for _, groupID := range []int{0, 1001} {
		t.Run(strconv.Itoa(groupID), func(t *testing.T) {
			db, statements := deviceGroupDryRunDB(t)
			repo := &DeviceRepository{db: db}
			device := FromModelDevice(&models.Device{ID: 7, OwnerID: 42, SSID: 1, GroupID: groupID})
			if err := repo.CreateDevice(device); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateDevice(device); err != nil {
				t.Fatal(err)
			}
			if len(*statements) != 2 {
				t.Fatalf("got %d statements, want insert and update", len(*statements))
			}
			var want driver.Value
			if groupID > 0 {
				want = int64(groupID)
			}
			for _, statement := range *statements {
				if !statement.hasGroup || statement.group != want {
					t.Fatalf("group_id parameter = %#v (present=%t), want %#v: %s", statement.group, statement.hasGroup, want, statement.sql)
				}
			}
			if got := device.ToModelDevice().GroupID; got != groupID {
				t.Fatalf("runtime group = %d, want %d", got, groupID)
			}
		})
	}
}

func TestDeviceGroupUpdatesUseNullableMySQLValue(t *testing.T) {
	for _, method := range []string{"fields", "owner_ssid"} {
		for _, groupID := range []int{0, 1001} {
			t.Run(method+"/"+strconv.Itoa(groupID), func(t *testing.T) {
				db, statements := deviceGroupDryRunDB(t)
				repo := &DeviceRepository{db: db}
				fields := map[string]interface{}{"group_id": groupID, "name": "device"}
				var err error
				if method == "fields" {
					err = repo.UpdateDeviceFields(7, fields)
				} else {
					err = repo.ChangeDeviceGroup(42, 1, groupID)
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(*statements) != 1 {
					t.Fatalf("got %d statements, want one update", len(*statements))
				}
				var want driver.Value
				if groupID > 0 {
					want = int64(groupID)
				}
				statement := (*statements)[0]
				if !statement.hasGroup || statement.group != want {
					t.Fatalf("group_id parameter = %#v (present=%t), want %#v: %s", statement.group, statement.hasGroup, want, statement.sql)
				}
				if fields["group_id"] != groupID {
					t.Fatal("updating device fields mutated the caller's map")
				}
			})
		}
	}
}

func TestDeviceUngroupedQueriesIncludeNullAndLegacyZero(t *testing.T) {
	for _, paginated := range []bool{false, true} {
		t.Run(strconv.FormatBool(paginated), func(t *testing.T) {
			db, statements := deviceGroupDryRunDB(t)
			repo := &DeviceRepository{db: db}
			var err error
			if paginated {
				_, _, err = repo.ListDevicesByGroupIDPaginated(0, 42, 20, 1)
			} else {
				_, err = repo.ListDevicesByGroupID(0)
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(*statements) == 0 {
				t.Fatal("no device queries were generated")
			}
			for _, statement := range *statements {
				if !strings.Contains(statement.sql, "devices.group_id IS NULL OR devices.group_id = 0") {
					t.Fatalf("query does not match ungrouped devices: %s", statement.sql)
				}
				if paginated && !strings.Contains(statement.sql, "(devices.group_id IS NULL OR devices.group_id = 0) AND devices.owner_id = ?") {
					t.Fatalf("ungrouped filter bypasses owner scope: %s", statement.sql)
				}
			}
		})
	}
}
