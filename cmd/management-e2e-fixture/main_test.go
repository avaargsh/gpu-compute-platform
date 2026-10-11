package main

import "testing"

func TestFixtureDSNRejectsProductionAndRemoteDatabases(t *testing.T) {
	for _, raw := range []string{
		"", "postgres://gpu:gpu@prod.company:5432/gpu_platform_browser_test?sslmode=disable",
		"postgres://gpu:gpu@localhost:5432/gpu_production?sslmode=disable",
		"postgres://gpu:gpu@127.0.0.1:5432/postgres?sslmode=disable",
		"postgres://gpu:gpu@localhost:5433/gpu_platform_browser_test?sslmode=disable",
		"postgres://gpu:gpu@db:5432/gpu_platform_browser_test?sslmode=disable",
		"mysql://gpu:gpu@localhost:5432/gpu_platform_browser_test",
	} {
		if err := validateFixtureDSN(raw); err == nil {
			t.Errorf("unsafe fixture database accepted")
		}
	}
	if err := validateFixtureDSN("postgres://gpu:gpu@localhost:5432/gpu_platform_browser_test?sslmode=disable"); err != nil {
		t.Fatal(err)
	}
}
