#!/bin/bash
cd "$(dirname "$0")"
echo "Starting ComfyUI-API-Modelscope..."
echo "Visit: http://127.0.0.1:3000/"
echo "Press Ctrl+C to stop."
echo ""

# Open browser after 3 seconds
sleep 3 && open http://127.0.0.1:3000/ &

python3 main.py

echo ""
echo "Server stopped."