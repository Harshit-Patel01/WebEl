package auth

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/opendeploy/opendeploy/internal/state"
	"go.uber.org/zap"
)

func TestJWTSecretPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")

	db, err := state.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}

	logger := zap.NewNop()
	a1 := New(db, time.Hour, 10, false, logger)
	secret1 := append([]byte(nil), a1.jwtSecret...)

	token, err := a1.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	stored, err := db.GetSetupState(jwtSecretKey)
	if err != nil || stored == "" {
		t.Fatalf("expected jwt_secret in setup_state, got %q err=%v", stored, err)
	}

	db.Close()

	db2, err := state.NewDB(dbPath)
	if err != nil {
		t.Fatalf("reopen NewDB: %v", err)
	}
	defer db2.Close()

	a2 := New(db2, time.Hour, 10, false, logger)
	if string(a2.jwtSecret) != string(secret1) {
		t.Fatalf("jwt secret changed after restart")
	}
	if !a2.ValidateToken(token) {
		t.Fatalf("token from first instance should validate on second instance")
	}
}

func TestOnboardingFlagPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")

	db, err := state.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	a := New(db, time.Hour, 10, false, zap.NewNop())

	if a.IsOnboardingComplete() {
		t.Fatal("onboarding should start incomplete on a fresh install")
	}
	if err := a.MarkOnboardingComplete(); err != nil {
		t.Fatalf("MarkOnboardingComplete: %v", err)
	}
	db.Close()

	db2, err := state.NewDB(dbPath)
	if err != nil {
		t.Fatalf("reopen NewDB: %v", err)
	}
	defer db2.Close()

	if !New(db2, time.Hour, 10, false, zap.NewNop()).IsOnboardingComplete() {
		t.Fatal("onboarding flag should survive restart")
	}
}
