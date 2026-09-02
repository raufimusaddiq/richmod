package queue

import (
	"errors"
	"testing"
)

type permanentTestError struct{}

func (permanentTestError) Error() string   { return "permanent" }
func (permanentTestError) Permanent() bool { return true }

func TestIsPermanentUnwrapsErrors(t *testing.T) {
	if isPermanent(errors.New("temporary")) {
		t.Fatal("ordinary error marked permanent")
	}
	if !isPermanent(errors.Join(errors.New("outer"), permanentTestError{})) {
		t.Fatal("wrapped permanent error was not recognized")
	}
}
