package recovery

import "testing"

func TestDatabaseNameAndURL(t *testing.T) {
	dsn := "postgres://hdms_prod:s3cret@db:5432/hdms_prod?sslmode=disable"
	name, err := DatabaseName(dsn)
	if err != nil || name != "hdms_prod" {
		t.Fatalf("DatabaseName = %q, %v", name, err)
	}
	other, err := DatabaseURL(dsn, "hdms_prod_restore_20261001t090000")
	if err != nil {
		t.Fatal(err)
	}
	if want := "postgres://hdms_prod:s3cret@db:5432/hdms_prod_restore_20261001t090000?sslmode=disable"; other != want {
		t.Fatalf("DatabaseURL = %q, want %q", other, want)
	}
	if _, err := DatabaseName("host=db dbname=hdms"); err == nil {
		t.Fatal("a keyword DSN must be refused: the worker needs URL form to name other databases")
	}
	if _, err := DatabaseName("postgres://db:5432/"); err == nil {
		t.Fatal("a DSN without a database must be refused")
	}
}
