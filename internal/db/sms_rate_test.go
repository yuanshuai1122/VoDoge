package db

import (
	"errors"
	"testing"
	"time"
)

func TestReserveSMSSendRollingHour(t *testing.T) {
	OpenTestDB(t)

	st, err := ReserveSMSSend(2, "dev-a", "+86100")
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	if st.Used != 1 || st.Remaining != 1 || st.Unlimited {
		t.Fatalf("after first: %+v", st)
	}

	st, err = ReserveSMSSend(2, "dev-b", "+86101")
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	if st.Used != 2 || st.Remaining != 0 {
		t.Fatalf("after second: %+v", st)
	}

	_, err = ReserveSMSSend(2, "dev-c", "+86102")
	if !IsSMSRateLimited(err) {
		t.Fatalf("third should be limited, err=%v", err)
	}
	var limited *SMSRateLimitedError
	if !errors.As(err, &limited) || limited.Used != 2 || limited.RetryAfterSeconds < 1 {
		t.Fatalf("limited payload: err=%v limited=%+v", err, limited)
	}

	got, err := GetSMSRateStatus(2)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got.Used != 2 || got.Remaining != 0 {
		t.Fatalf("status=%+v", got)
	}
}

func TestReserveSMSSendUnlimitedStillCounts(t *testing.T) {
	OpenTestDB(t)
	if _, err := ReserveSMSSend(0, "dev", "+1"); err != nil {
		t.Fatalf("unlimited reserve: %v", err)
	}
	if _, err := ReserveSMSSend(0, "dev", "+2"); err != nil {
		t.Fatalf("second unlimited: %v", err)
	}
	st, err := GetSMSRateStatus(0)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.Unlimited || st.Used != 2 || st.Remaining != 0 {
		t.Fatalf("status=%+v", st)
	}
}

func TestReserveSMSSendIgnoresDeletedHistory(t *testing.T) {
	OpenTestDB(t)
	if _, err := ReserveSMSSend(1, "dev", "+1"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := SaveSMS("imsi-x", "me", "+1", "hi", 2, 2, time.Now()); err != nil {
		t.Fatalf("SaveSMS: %v", err)
	}
	if _, _, _, err := DeleteSMSByID(1); err != nil && err != ErrSMSNotFound {
		// id 可能不是 1；删不掉不影响本断言
		_ = err
	}
	if _, err := ReserveSMSSend(1, "dev", "+2"); !IsSMSRateLimited(err) {
		t.Fatalf("deleting sms history must not restore quota, err=%v", err)
	}
}

func TestNewSMSRateStatus(t *testing.T) {
	st := NewSMSRateStatus(20, 3)
	if st.HourlyLimit != 20 || st.Used != 3 || st.Remaining != 17 || st.Unlimited || st.WindowSeconds != 3600 {
		t.Fatalf("%+v", st)
	}
	st = NewSMSRateStatus(0, 4)
	if !st.Unlimited || st.Remaining != 0 {
		t.Fatalf("unlimited %+v", st)
	}
}
