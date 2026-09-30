package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprint_OrderIndependentAndDirectionSensitive(t *testing.T) {
	if Fingerprint("push", []string{"A", "B"}) != Fingerprint("push", []string{"B", "A"}) {
		t.Fatal("fingerprint should not depend on key order")
	}
	if Fingerprint("push", []string{"A"}) == Fingerprint("pull", []string{"A"}) {
		t.Fatal("fingerprint should differ by direction")
	}
	if Fingerprint("push", []string{"A"}) == Fingerprint("push", []string{"A", "B"}) {
		t.Fatal("fingerprint should differ when the key set differs")
	}
}

func TestSnoozeRoundTripAndClearByDirection(t *testing.T) {
	dir := t.TempDir()
	fpPush, fpPull := Fingerprint("push", []string{"A"}), Fingerprint("pull", []string{"B"})

	if IsSnoozed(dir, "push", fpPush) {
		t.Fatal("nothing should be snoozed initially")
	}
	if err := Snooze(dir, "push", fpPush); err != nil {
		t.Fatal(err)
	}
	if err := Snooze(dir, "pull", fpPull); err != nil {
		t.Fatal(err)
	}
	if !IsSnoozed(dir, "push", fpPush) || !IsSnoozed(dir, "pull", fpPull) {
		t.Fatal("expected both to be snoozed")
	}

	if err := ClearSnoozed(dir, "push"); err != nil {
		t.Fatal(err)
	}
	if IsSnoozed(dir, "push", fpPush) {
		t.Fatal("push snooze should have been cleared")
	}
	if !IsSnoozed(dir, "pull", fpPull) {
		t.Fatal("clearing push must not touch pull")
	}

	if err := ClearSnoozed(dir, "pull"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, snoozeFileName)); err == nil {
		t.Fatal("snooze file should be removed once empty")
	}
	if err := ClearSnoozed(dir, "push"); err != nil {
		t.Fatalf("clearing with no snooze file must be a no-op, got %v", err)
	}
}

func TestLocalKeys(t *testing.T) {
	dir := t.TempDir()
	if keys, err := LocalKeys(dir); err != nil || len(keys) != 0 {
		t.Fatalf("expected empty set, got %v, %v", keys, err)
	}
	if err := MarkLocal(dir, "X"); err != nil {
		t.Fatal(err)
	}
	if err := MarkLocal(dir, "X"); err != nil { // idempotent
		t.Fatal(err)
	}
	keys, _ := LocalKeys(dir)
	if !keys["X"] || len(keys) != 1 {
		t.Fatalf("unexpected keys: %v", keys)
	}
}

func TestDisabledFlag(t *testing.T) {
	dir := t.TempDir()
	if IsDisabled(dir) {
		t.Fatal("should start enabled")
	}
	if err := SetDisabled(dir, true); err != nil || !IsDisabled(dir) {
		t.Fatalf("expected disabled: %v", err)
	}
	if err := SetDisabled(dir, false); err != nil || IsDisabled(dir) {
		t.Fatalf("expected enabled again: %v", err)
	}
	if err := SetDisabled(dir, false); err != nil {
		t.Fatalf("re-enabling when already enabled must not fail: %v", err)
	}
}
