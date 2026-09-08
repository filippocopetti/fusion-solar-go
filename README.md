# FusionSolar Go

Go port of the uploaded `fusion_solar_py` 0.1.2 package.

The port keeps the same FusionSolar HTTP endpoints and the main client operations, while using Go's standard library (`net/http`, cookie jars, `crypto/rsa`, `encoding/json`, etc.).

## Included

- Login against the v2/v3 FusionSolar authentication endpoints
- FusionSolar public-key password encryption (RSA-OAEP/SHA-384)
- Session/cookie reuse
- Keep-alive and session checks
- Plant/station listing and plant IDs
- Current plant and aggregate power data
- Device IDs
- Historical and real-time device data
- Alarm data
- Plant energy-flow and daily energy-balance data
- Last-value extraction for plant time series
- Battery IDs, status, daily statistics and module statistics
- Inverter daily statistics
- Optimizer statistics
- Active power control
- CTC prefix beam-search decoder
- Pluggable CAPTCHA solver interface

## Important CAPTCHA note

The Python archive contains the ONNX CAPTCHA implementation, but it does not contain the `captcha_huawei.onnx` model. This Go port therefore exposes:

```go
type CaptchaSolver interface {
    SolveCaptcha(image []byte) (string, error)
}
```

You can plug in an ONNX runtime implementation without making the core HTTP client depend on a particular ONNX binding. If no solver is supplied, CAPTCHA-protected logins return an authentication error instead of silently bypassing the CAPTCHA.

## Example

```go
package main

import (
    "fmt"
    "log"

    fusionsolar "github.com/yourname/fusion-solar-go"
)

func main() {
    client, err := fusionsolar.NewClient(
        "my_user",
        "my_password",
        "region01eu5",
        nil, // cookies; pass a saved cookie map to reuse a session
        nil, // optional CAPTCHA solver
    )
    if err != nil {
        log.Fatal(err)
    }

    status, err := client.GetPowerStatus()
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Current power: %.3f kW\n", status.CurrentPowerKW)
    fmt.Printf("Today: %.3f kWh\n", status.EnergyTodayKWh)
    fmt.Printf("Total: %.3f kWh\n", status.EnergyKWh)

    plants, err := client.GetStationList()
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Plants: %d\n", len(plants))
}
```

Change the module path in `go.mod` to your own repository path before publishing.

## Cookie reuse

```go
cookies := client.GetCookies()

newClient, err := fusionsolar.NewClient(
    "my_user",
    "my_password",
    "region01eu5",
    cookies,
    nil,
)
```

Cookies are represented as `map[string]string`, matching the Python package's public API closely.

## API mapping

| Python | Go |
|---|---|
| `FusionSolarClient(...)` | `NewClient(...)` |
| `get_power_status()` | `GetPowerStatus()` |
| `get_station_list()` | `GetStationList()` |
| `get_plant_ids()` | `GetPlantIDs()` |
| `get_current_plant_data()` | `GetCurrentPlantData()` |
| `get_plant_stats()` | `GetPlantStats()` |
| `get_last_plant_data()` | `GetLastPlantData()` |
| `get_plant_flow()` | `GetPlantFlow()` |
| `get_device_ids()` | `GetDeviceIDs()` |
| `get_historical_data()` | `GetHistoricalData()` |
| `get_real_time_data()` | `GetRealTimeData()` |
| `get_alarm_data()` | `GetAlarmData()` |
| `get_battery_ids()` | `GetBatteryIDs()` |
| `get_battery_basic_stats()` | `GetBatteryBasicStats()` |
| `get_battery_day_stats()` | `GetBatteryDayStats()` |
| `get_inverter_day_stats()` | `GetInverterDayStats()` |
| `get_battery_module_stats()` | `GetBatteryModuleStats()` |
| `get_battery_status()` | `GetBatteryStatus()` |
| `get_optimizer_stats()` | `GetOptimizerStats()` |
| `active_power_control()` | `ActivePowerControl()` |
| `get_cookies()` | `GetCookies()` |
| `is_session_active()` | `IsSessionActive()` |
| `keep_alive()` | `KeepAlive()` |
| `log_out()` | `LogOut()` |
| `encrypt_password()` | `EncryptPassword()` |
| `get_secure_random()` | `SecureRandomHex()` |
| `ctc_decoder.decode()` | `DecodeCTC()` |

## Build

```bash
gofmt -w *.go
go test ./...
go vet ./...
```

The package currently has no third-party Go dependencies.
