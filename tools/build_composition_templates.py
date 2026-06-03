#!/usr/bin/env python3
"""Convert original author's markdown prompt templates to composition.json.

Reads:  ../../Original-Infinite-Canvas/static/system-prompts/infinite-canvas-prompt-templates.md
Writes: ../static/system-prompts/templates/composition.json

Run from tools/ directory or project root:
  python tools/build_composition_templates.py
"""

import json
import os
import re
import sys

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_ROOT = os.path.join(SCRIPT_DIR, '..')
SOURCE_MD = os.path.join(PROJECT_ROOT, '..', 'Original-Infinite-Canvas',
                          'static', 'system-prompts', 'infinite-canvas-prompt-templates.md')
OUTPUT_JSON = os.path.join(PROJECT_ROOT, 'static', 'system-prompts', 'templates', 'composition.json')

GROUP_COLORS = {
    'A': '#8B5CF6', 'B': '#3B82F6', 'C': '#10B981',
    'D': '#F59E0B', 'E': '#EC4899', 'F': '#06B6D4',
}

# Manual short IDs for the 10 known presets, in order
PRESET_IDS = [
    'multi_angle_3x3',
    'multi_angle_3x3_4k',
    'story_2x2',
    'face_three_view',
    'product_three_view',
    'storyboard_5x5',
    'lighting_compare',
    'character_sheet',
    'expression_2x3',
    'panorama_360',
]


def extract_presets(md_text: str) -> list[dict]:
    """Parse the markdown file into a list of preset dicts."""
    # Split by "## 预设N：" headings
    sections = re.split(r'^## 预设\d+：', md_text, flags=re.MULTILINE)
    # Skip the first chunk (header/overview before first preset)
    sections = sections[1:]

    presets = []
    for idx, section in enumerate(sections, 1):
        preset = parse_preset_section(section, idx)
        if preset:
            presets.append(preset)
    return presets


def parse_preset_section(section: str, index: int) -> dict | None:
    """Parse a single preset section into structured data."""
    lines = section.strip().split('\n')

    # Title is the first line
    name = lines[0].strip() if lines else f'Preset {index}'

    # Extract scene description
    scene_match = re.search(r'###\s*适用场景\s*\n(.+?)(?=\n###|\n##|\Z)', section, re.DOTALL)
    scene = scene_match.group(1).strip() if scene_match else ''

    # Extract positive prompt
    pos_match = re.search(r'###\s*正向提示词\s*\n```\s*\n(.+?)\n```', section, re.DOTALL)
    positive = pos_match.group(1).strip() if pos_match else ''

    # Extract negative prompt
    neg_match = re.search(r'###\s*负向提示词\s*\n```\s*\n(.+?)\n```', section, re.DOTALL)
    negative = neg_match.group(1).strip() if neg_match else ''

    # Extract platform params
    params = {}
    param_match = re.search(r'###\s*平台参数建议\s*\n(.+?)(?=\n###|\n##|\Z)', section, re.DOTALL)
    if param_match:
        param_text = param_match.group(1).strip()
        for line in param_text.split('\n'):
            line = line.strip().lstrip('- ').strip()
            if not line:
                continue
            # Parse "Midjourney: --ar 1:1 --style raw --s 50"
            m = re.match(r'^\*\*?(.+?)\*\*?:\s*(.+)$', line)
            if m:
                value = m.group(2).strip()
                # Strip enclosing backticks
                value = re.sub(r'`([^`]+)`', r'\1', value)
                params[m.group(1).strip()] = value

    # Extract placeholders from positive prompt: [xxx]
    placeholders = re.findall(r'\[([^\]]+)\]', positive)
    # Deduplicate while preserving order
    seen = set()
    unique_placeholders = []
    for p in placeholders:
        if p not in seen:
            seen.add(p)
            unique_placeholders.append(p)

    # Use manual short ID if available, else fallback
    preset_id = PRESET_IDS[index - 1] if index - 1 < len(PRESET_IDS) else f'preset_{index}'

    return {
        'id': preset_id,
        'name': name,
        'scene': scene,
        'positive': positive,
        'negative': negative,
        'params': params,
        'placeholders': unique_placeholders,
    }


def main():
    if not os.path.exists(SOURCE_MD):
        print(f'Source not found: {SOURCE_MD}', file=sys.stderr)
        print('Make sure Original-Infinite-Canvas is cloned alongside this project.', file=sys.stderr)
        sys.exit(1)

    with open(SOURCE_MD, 'r', encoding='utf-8') as f:
        md_text = f.read()

    presets = extract_presets(md_text)

    output = {
        'version': '1.0',
        'source': 'infinite-canvas-prompt-templates.md v2.1',
        'presets': presets,
    }

    os.makedirs(os.path.dirname(OUTPUT_JSON), exist_ok=True)
    with open(OUTPUT_JSON, 'w', encoding='utf-8') as f:
        json.dump(output, f, ensure_ascii=False, indent=2)

    print(f'Converted {len(presets)} presets → {OUTPUT_JSON}')


if __name__ == '__main__':
    main()
