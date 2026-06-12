import asyncio
import json

from app.models import CanvasSaveRequest
from app.ws import ConnectionManager


class DummySocket:
    def __init__(self):
        self.messages = []

    async def send_text(self, message):
        self.messages.append(message)


def test_canvas_save_request_accepts_client_id_and_settings():
    payload = CanvasSaveRequest(
        title="demo",
        icon="layers",
        nodes=[],
        connections=[],
        viewport={},
        logs=[],
        settings={"snap": True},
        base_updated_at=123,
        client_id="canvas_abc",
    )

    assert payload.settings == {"snap": True}
    assert payload.client_id == "canvas_abc"


def test_broadcast_canvas_updated_message():
    async def run():
        manager = ConnectionManager()
        socket = DummySocket()
        manager.active_connections.append(socket)
        await manager.broadcast_canvas_updated("canvas1", 456, "canvas_abc")
        return socket.messages

    messages = asyncio.run(run())

    assert len(messages) == 1
    assert json.loads(messages[0]) == {
        "type": "canvas_updated",
        "canvas_id": "canvas1",
        "updated_at": 456,
        "client_id": "canvas_abc",
    }
