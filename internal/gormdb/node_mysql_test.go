package gormdb

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const nodeMySQLTestDSNEnv = "DRAARL_TEST_MYSQL_DSN"

func TestNodeCredentialLifecycleMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(nodeMySQLTestDSNEnv))
	if dsn == "" {
		t.Skip("set " + nodeMySQLTestDSNEnv + " to run the MySQL credential lifecycle test")
	}
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MySQL test DSN: %v", err)
	}
	if !strings.HasPrefix(parsed.DBName, "draarl_test_") {
		t.Fatalf("refusing non-test database %q; name must start with draarl_test_", parsed.DBName)
	}
	parsed.ParseTime = true
	db, err := gorm.Open(gormmysql.Open(parsed.FormatDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open MySQL test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&Server{}); err != nil {
		t.Fatalf("migrate test servers table: %v", err)
	}
	repo := &ServerRepository{db: db}
	now := time.Now().UTC().Truncate(time.Second)
	suffix := fmt.Sprintf("%x", now.UnixNano())
	if len(suffix) > 20 {
		suffix = suffix[len(suffix)-20:]
	}
	createdNodeIDs := make([]string, 0, 3)
	t.Cleanup(func() {
		if len(createdNodeIDs) > 0 {
			_ = db.Unscoped().Where("node_id IN ?", createdNodeIDs).Delete(&Server{}).Error
		}
	})
	createNode := func(label string, status int, registration, current string, registrationExpiry *time.Time) *Server {
		t.Helper()
		nodeID := "edge-" + label + "-" + suffix
		createdNodeIDs = append(createdNodeIDs, nodeID)
		node := &Server{
			Name: label, DisplayName: label, Status: status, ServerType: 3, NodeID: &nodeID,
			NodeRegistrationTokenHash: credentialTestHash(registration),
			NodeRegistrationExpiresAt: registrationExpiry,
			NodeTokenHash:             credentialTestHash(current),
		}
		if registration == "" {
			node.NodeRegistrationTokenHash = ""
		}
		if current == "" {
			node.NodeTokenHash = ""
		}
		if err := db.Create(node).Error; err != nil {
			t.Fatalf("create %s node: %v", label, err)
		}
		return node
	}

	registration := "registration-" + suffix
	registrationExpiry := now.Add(time.Minute)
	node := createNode("lifecycle", 1, registration, "", &registrationExpiry)
	candidates := []string{"issued-a-" + suffix, "issued-b-" + suffix}
	type enrollmentAttempt struct {
		credential string
		result     NodeAuthenticationResult
		err        error
	}
	attempts := make(chan enrollmentAttempt, len(candidates))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, candidate := range candidates {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, authErr := repo.AuthenticateNode(
				*node.NodeID, credentialTestHash(registration), credentialTestHash(candidate), now,
			)
			attempts <- enrollmentAttempt{credential: candidate, result: result, err: authErr}
		}()
	}
	close(start)
	wg.Wait()
	close(attempts)
	var issuedCredential string
	accepted, rejected := 0, 0
	for attempt := range attempts {
		switch {
		case attempt.err == nil && attempt.result.Accepted && attempt.result.IssueCredential && attempt.result.CredentialEpoch == 1:
			accepted++
			issuedCredential = attempt.credential
		case errors.Is(attempt.err, ErrNodeCredentialInvalid):
			rejected++
		default:
			t.Fatalf("unexpected concurrent enrollment result: result=%#v err=%v", attempt.result, attempt.err)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("concurrent registration accepted=%d rejected=%d", accepted, rejected)
	}
	if result, err := repo.AuthenticateNode(*node.NodeID, credentialTestHash(registration), credentialTestHash("unused"), now); !errors.Is(err, ErrNodeCredentialInvalid) || result.Accepted {
		t.Fatalf("consumed registration token was reused: result=%#v err=%v", result, err)
	}
	if result, err := repo.AuthenticateNode(*node.NodeID, credentialTestHash(issuedCredential), credentialTestHash("unused"), now); err != nil || !result.Accepted || result.IssueCredential || result.CredentialEpoch != 1 {
		t.Fatalf("issued credential authentication failed: result=%#v err=%v", result, err)
	}
	var enrolled Server
	if err := db.Where("id = ?", node.ID).First(&enrolled).Error; err != nil {
		t.Fatal(err)
	}
	if enrolled.NodeTokenHash != credentialTestHash(issuedCredential) || enrolled.NodeTokenHash == issuedCredential ||
		enrolled.NodeRegistrationTokenHash != "" || enrolled.NodeRegistrationExpiresAt != nil || enrolled.NodeRegisteredAt == nil || enrolled.NodeCredentialEpoch != 1 {
		t.Fatalf("unexpected persisted enrollment state: %#v", enrolled)
	}

	otherRegistration := "other-registration-" + suffix
	otherExpiry := now.Add(time.Minute)
	other := createNode("binding", 1, otherRegistration, "", &otherExpiry)
	if result, err := repo.AuthenticateNode(*other.NodeID, credentialTestHash(registration), credentialTestHash("other-issued"), now); !errors.Is(err, ErrNodeCredentialInvalid) || result.Accepted {
		t.Fatalf("credential was accepted for another node: result=%#v err=%v", result, err)
	}

	expiredRegistration := "expired-registration-" + suffix
	expiredAt := now.Add(-time.Second)
	expired := createNode("expired", 1, expiredRegistration, "", &expiredAt)
	if result, err := repo.AuthenticateNode(*expired.NodeID, credentialTestHash(expiredRegistration), credentialTestHash("expired-issued"), now); !errors.Is(err, ErrNodeCredentialInvalid) || result.Accepted {
		t.Fatalf("expired registration token was accepted: result=%#v err=%v", result, err)
	}

	rotatedCredential := "rotated-" + suffix
	rotation, err := repo.RotateNodeCredential(node.ID, credentialTestHash(rotatedCredential), now, 10*time.Second)
	if err != nil || rotation.CredentialEpoch != 2 || !rotation.PreviousValidUntil.Equal(now.Add(10*time.Second)) {
		t.Fatalf("credential rotation=%#v err=%v", rotation, err)
	}
	if result, err := repo.AuthenticateNode(*node.NodeID, credentialTestHash(issuedCredential), credentialTestHash("unused"), now.Add(9*time.Second)); err != nil || !result.Accepted || result.CredentialEpoch != 2 {
		t.Fatalf("previous credential failed inside grace: result=%#v err=%v", result, err)
	}
	if result, err := repo.AuthenticateNode(*node.NodeID, credentialTestHash(issuedCredential), credentialTestHash("unused"), now.Add(11*time.Second)); !errors.Is(err, ErrNodeCredentialInvalid) || result.Accepted {
		t.Fatalf("previous credential survived grace: result=%#v err=%v", result, err)
	}
	if result, err := repo.AuthenticateNode(*node.NodeID, credentialTestHash(rotatedCredential), credentialTestHash("unused"), now.Add(11*time.Second)); err != nil || !result.Accepted || result.CredentialEpoch != 2 {
		t.Fatalf("rotated credential authentication failed: result=%#v err=%v", result, err)
	}

	revokedNodeID, revokedEpoch, err := repo.RevokeNodeCredentials(node.ID)
	if err != nil || revokedNodeID != *node.NodeID || revokedEpoch != 3 {
		t.Fatalf("credential revocation node=%q epoch=%d err=%v", revokedNodeID, revokedEpoch, err)
	}
	for label, credential := range map[string]string{"current": rotatedCredential, "previous": issuedCredential, "registration": registration} {
		if result, authErr := repo.AuthenticateNode(*node.NodeID, credentialTestHash(credential), credentialTestHash("unused"), now); !errors.Is(authErr, ErrNodeCredentialInvalid) || result.Accepted {
			t.Fatalf("%s credential survived revocation: result=%#v err=%v", label, result, authErr)
		}
	}

	disabledCredential := "disabled-" + suffix
	disabled := createNode("disabled", 0, "", disabledCredential, nil)
	if result, err := repo.AuthenticateNode(*disabled.NodeID, credentialTestHash(disabledCredential), credentialTestHash("unused"), now); !errors.Is(err, ErrNodeDisabled) || result.Accepted {
		t.Fatalf("disabled node authenticated: result=%#v err=%v", result, err)
	}
	if _, err := repo.RotateNodeCredential(disabled.ID, credentialTestHash("disabled-rotation"), now, time.Minute); !errors.Is(err, ErrNodeDisabled) {
		t.Fatalf("disabled node credential rotated: %v", err)
	}
}

func credentialTestHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
