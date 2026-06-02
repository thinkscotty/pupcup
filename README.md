# PupCup

A single Go binary for a Raspberry Pi Zero 2W that drives a physical button device and serves a local-network web app for tracking dog feedings.

See [pupcup_build_plan.md](pupcup_build_plan.md) for architecture and [pupcup_hardware_build.md](pupcup_hardware_build.md) for the hardware build.

## Quick start (laptop)

```sh
go build ./...
go test ./...
go run ./cmd/pupcup --config deploy/config.example.yaml
```

## Cross-compile + deploy to Pi

```sh
TARGET=pupcup@pupcup.local ./deploy/deploy.sh
```

## License

Source code is licensed under the [MIT License](LICENSE). Hardware design
documentation (wiring, BOM, pinout, layouts, bring-up procedures) is licensed
separately under [Creative Commons Attribution 4.0 International (CC BY 4.0)](LICENSE-HARDWARE).
