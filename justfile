# wtwlt task runner.
# Firmware tasks are namespaced:  `just firmware <recipe>`  (e.g. just firmware build).
# Future phases will add their own modules here (e.g. `mod server`, `mod web`).

mod firmware

# List available recipes and modules
default:
    @just --list
