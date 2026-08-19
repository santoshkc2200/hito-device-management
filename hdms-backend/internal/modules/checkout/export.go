package checkout

import "github.com/hito-hospital/hdms/internal/modules/checkout/internal/machine"

// ExportMachineJSON returns the formatted JSON representation of the shared machine definition.
func ExportMachineJSON() ([]byte, error) {
	return machine.ExportMachineJSON()
}

// ExportScenariosJSON returns the formatted JSON representation of the shared scenarios fixture.
func ExportScenariosJSON() ([]byte, error) {
	return machine.ExportScenariosJSON()
}
