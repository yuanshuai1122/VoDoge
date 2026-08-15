package esim

import "testing"

func TestIsProfileEnabled(t *testing.T) {
	err := NewDeleteProfileError(DeleteProfileErrorProfileEnabled, "当前启用的 Profile 不能删除", nil)
	if !IsProfileEnabled(err) {
		t.Fatal("IsProfileEnabled()=false")
	}
	if IsProfileEnabled(NewDeleteProfileError(DeleteProfileErrorProfileNotFound, "gone", nil)) {
		t.Fatal("not-found must not classify as enabled")
	}
}
