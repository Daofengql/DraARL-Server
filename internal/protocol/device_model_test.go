package protocol

import "testing"

func TestCurrentHardwareDeviceModels(t *testing.T) {
	want := []string{"BoxV1-RF", "BoxV1-EX", "Box32-ANA", "Box32-DIG", "Box32-MIX"}
	for index, name := range want {
		model := byte(index + 1)
		if !IsSelectableHardwareDevModel(model) || !IsFirmwareSupportedDevModel(model) {
			t.Fatalf("model %d is missing from the hardware/release lists", model)
		}
		if got := GetDevModelName(model); got != name {
			t.Fatalf("model %d name = %q, want %q", model, got, name)
		}
	}
	if DraARLDevModelESP32Radio != 1 || DraARLDevModelESP32NoRadio != 2 {
		t.Fatal("BoxV1 compatibility aliases changed wire IDs")
	}
	if IsSelectableHardwareDevModel(6) || IsFirmwareSupportedDevModel(6) {
		t.Fatal("model 6 must remain reserved")
	}
}
