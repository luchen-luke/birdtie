# Client lazy-display log archive

Provenance: **RECONSTRUCTED_FROM_LOG**. These files preserve already-existing logs; this archive operation ran no tests and accessed no production systems.

| Log | Observed result |
| --- | --- |
| lazy-display-client-test01.log | FAIL: 5 existing tests passed; the new lazy test file did not compile because LazyFixture.read conflicted with BaseClient.read. |
| lazy-display-client-test02.log | PASS: 4 lazy-display test cases. |
| lazy-display-client-test03.log | PASS: the same 4 lazy-display test cases again. |
| lazy-display-client-analyze01.log | DIAGNOSTICS_CAPTURED: 42 INFO, 0 WARNING, 0 ERROR; process exit status not captured. |

There are 9 distinct passing test cases across the logs (13 passing executions including the repeated 4 cases). The failed test01 load is retained; it is not converted into a pass by later results.

Root identifies test03 as the final run after the aspect-ratio tolerance change. That sequence is caller context; the log has no historical source hash to prove the exact tested diff. The 3 subsequent real-panel retirement/video-renewal regressions are **NOT_RUN_IN_THESE_LOGS**.

Exact commands, process cwd, exit codes, runtime versions and execution timestamps are **UNKNOWN**. The logs show absolute files under D:/Project/birdtie/apps/client, but that does not prove cwd. Searches in the root task work scripts and repository work scripts found no runner binding these four filenames to command metadata. The later test_lazy_retirement.py was not substituted for the original commands.

receipt.json records original paths, byte counts and SHA-256 hashes for the verbatim log copies. These hashes identify the evidence bytes only. Current source hashes are intentionally not presented as historical test-source hashes. This archive is not device, browser, production, real video-playback or full publishing acceptance evidence.
