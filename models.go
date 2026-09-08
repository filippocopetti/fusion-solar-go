package fusionsolar

type PowerStatus struct {
	CurrentPowerKW float64 `json:"current_power_kw"`
	EnergyTodayKWh float64 `json:"energy_today_kwh"`
	EnergyKWh      float64 `json:"energy_kwh"`
}
type BatteryStatus struct {
	StateOfCharge            float64 `json:"state_of_charge"`
	RatedCapacity            float64 `json:"rated_capacity"`
	OperatingStatus          string  `json:"operating_status"`
	BackupTime               string  `json:"backup_time"`
	BusVoltage               float64 `json:"bus_voltage"`
	TotalChargedTodayKWh     float64 `json:"total_charged_today_kwh"`
	TotalDischargedTodayKWh  float64 `json:"total_discharged_today_kwh"`
	CurrentChargeDischargeKW float64 `json:"current_charge_discharge_kw"`
}
type Device struct {
	Type     string `json:"type"`
	DeviceDN string `json:"deviceDn"`
	ID       string `json:"id,omitempty"`
}
type LastValue struct {
	Time  string   `json:"time"`
	Value *float64 `json:"value"`
}
