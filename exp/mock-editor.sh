#!/bin/bash
# mock-editor.sh — simulates an editor by appending a marker line
# Usage: mock-editor.sh <filepath>
echo "MOCK_EDIT_$(date +%s)" >> "$1"
