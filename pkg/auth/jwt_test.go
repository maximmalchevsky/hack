package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIssueAndParseAccess(t *testing.T) {
	m := NewManager("test-secret", 15*time.Minute, 7*24*time.Hour)
	uid := uuid.New()
	eid := uuid.New()

	access, refresh, err := m.Issue(uid, eid, "manager")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if access == "" || refresh == "" {
		t.Fatal("пустые токены")
	}
	if access == refresh {
		t.Error("access и refresh совпали")
	}

	claims, err := m.ParseAccess(access)
	if err != nil {
		t.Fatalf("ParseAccess: %v", err)
	}
	if claims.UserID != uid {
		t.Errorf("UserID = %v, want %v", claims.UserID, uid)
	}
	if claims.EmployeeID != eid {
		t.Errorf("EmployeeID = %v, want %v", claims.EmployeeID, eid)
	}
	if claims.Role != "manager" {
		t.Errorf("Role = %q, want manager", claims.Role)
	}
}

func TestTokenTypeMismatch(t *testing.T) {
	m := NewManager("test-secret", 15*time.Minute, 7*24*time.Hour)
	access, refresh, _ := m.Issue(uuid.New(), uuid.New(), "employee")

	if _, err := m.ParseAccess(refresh); err == nil {
		t.Error("ParseAccess принял refresh-токен")
	}
	if _, err := m.ParseRefresh(access); err == nil {
		t.Error("ParseRefresh принял access-токен")
	}
}

func TestParseRejectsWrongSecret(t *testing.T) {
	m1 := NewManager("secret-1", time.Minute, time.Hour)
	m2 := NewManager("secret-2", time.Minute, time.Hour)
	access, _, _ := m1.Issue(uuid.New(), uuid.New(), "admin")

	if _, err := m2.Parse(access); err == nil {
		t.Error("токен, подписанный другим секретом, не должен валидироваться")
	}
}

func TestParseRejectsExpired(t *testing.T) {
	m := NewManager("test-secret", -time.Minute, -time.Minute)
	access, _, _ := m.Issue(uuid.New(), uuid.New(), "hr")
	if _, err := m.ParseAccess(access); err == nil {
		t.Error("истёкший токен должен отклоняться")
	}
}
