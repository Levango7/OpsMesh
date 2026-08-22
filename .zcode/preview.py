#!/usr/bin/env python3
"""Preview before/after for a sample of files to verify regex safety."""
import sys
sys.path.insert(0, r"F:\Nexus\OpsMesh\.zcode")
import importlib.util
spec = importlib.util.spec_from_file_location("deai", r"F:\Nexus\OpsMesh\.zcode\deai.py")
deai = importlib.util.module_from_spec(spec)
spec.loader.exec_module(deai)

SAMPLES = [
    r"F:\Nexus\OpsMesh\internal\config\config.go",
    r"F:\Nexus\OpsMesh\internal\agent\agent.go",
    r"F:\Nexus\OpsMesh\internal\store\sql_audits.go",
    r"F:\Nexus\OpsMesh\docs\module-design.md",
    r"F:\Nexus\OpsMesh\README.md",
    r"F:\Nexus\OpsMesh\internal\agent\security_p02_test.go",
]

import pathlib
for path in SAMPLES:
    p = pathlib.Path(path)
    if not p.exists():
        print(f"!! missing {path}")
        continue
    text = p.read_text(encoding="utf-8")
    new, counts = deai.process_text(text, is_md=(p.suffix.lower() == ".md"))
    if new == text:
        print(f"== {p.name}: NO CHANGE")
        continue
    print(f"\n{'='*70}\n== {p.name}  counts={counts}")
    old_lines = text.splitlines()
    new_lines = new.splitlines()
    shown = 0
    # show first 12 changed lines with context
    import difflib
    for tag, i1, i2, j1, j2 in difflib.SequenceMatcher(None, old_lines, new_lines).get_opcodes():
        if tag == "equal":
            continue
        for i in range(i1, i2):
            if shown >= 14:
                break
            print(f"  - {old_lines[i].rstrip()}")
            shown += 1
        for j in range(j1, j2):
            if shown >= 14:
                break
            print(f"  + {new_lines[j].rstrip()}")
            shown += 1
        if shown >= 14:
            print("  ... (truncated)")
            break
