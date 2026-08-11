package db

import (
	"testing"
)

func TestCheckSchemaDevicesTable(t *testing.T) {
	OpenTestDB(t)
	if !DB.Migrator().HasTable(&Device{}) {
		t.Fatal("devices table missing after migrate")
	}
	if !DB.Migrator().HasTable(&SMS{}) {
		t.Fatal("sms table missing after migrate")
	}
}
