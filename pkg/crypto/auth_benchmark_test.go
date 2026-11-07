package crypto

import "testing"

// Measures the existing, intentional device-password scheme without changing it.
func BenchmarkVerifyDevicePassword(b *testing.B) {
	if err := InitAES("0123456789abcdef0123456789abcdef"); err != nil {
		b.Fatal(err)
	}
	stored, err := Encrypt("Bench12345")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		match, legacy, err := VerifyDevicePassword(stored, "Bench12345")
		if err != nil || !match || legacy {
			b.Fatalf("password verification failed: %v", err)
		}
	}
}
