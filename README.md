# WaveLab

WaveLab is a local HTTP service for tow-tank experiments. The original equipment and interlock pages expose a fixed commissioning diagnostic scenario. Operator measurements are a separate live data path.

Set `WAVELAB_DATA_DIR` to choose the archive directory (default `data/wavelab`). `POST /api/measured-runs` accepts `name`, `model_name`, `section`, and `carriage`. `POST /api/measured-runs/{id}/evidence` accepts `kind` (`wave_time` or `force`), `source_id`, finite `value`, and `recorded_at` in RFC3339 form. `GET /api/measured-runs`, `GET /api/measured-runs/{id}`, and `GET /api/measured-runs/{id}/analysis` return the supplied records and wave/force summary. Measurements are committed to `operator-measurements.json` before they are published in memory, and are reloaded when the service restarts. The archive does not automatically grant a safety permit or drive hardware.

The `internal/probe`, `internal/force`, `internal/permit`, `internal/carriage`, `internal/track`, `internal/wave`, `internal/absorber`, and `internal/journal` packages provide component logic for further work. Their diagnostic use does not imply that the original HTTP pages accept real sensor streams.

## Comparison groups

Repeatability checks are explicit, not averaged. `POST /api/comparison-groups` accepts `name` and `run_ids` (at least two existing measured runs, no duplicates); `PUT /api/comparison-groups/{id}` revises the selection, and `GET /api/comparison-groups` / `GET /api/comparison-groups/{id}` return the stored definitions. Groups are committed to `comparison-groups.json` next to the measurement archive and reloaded on restart.

`GET /api/comparison-groups/{id}/analysis` compares the member runs side by side without merging them into a score:

- `condition_differences` lists every condition field (`model_name`, `section`, `carriage`) whose value differs across the runs, with each run's actual value. Any difference marks the group `comparable: false` and is explained in `findings`; the per-run numbers are still shown so personnel can judge the cause.
- `runs` gives each run's own wave summary (count, mean, RMS, extremes, span, sources), force summary (count, mean, min, max), and evidence coverage.
- `wave` reports the spread of per-run means and RMS values plus a per-source comparison naming the runs that lack each source; `force` reports the mean spread and the runs without force evidence.
- `coverage_gaps` lists, per run, the wave/force sources other runs recorded but this one did not, and any evidence that was not persisted.

The analysis never produces an aggregated repeatability score: it presents per-run values, spreads, coverage gaps and condition differences for personnel to interpret.
