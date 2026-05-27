"""所有 Pydantic 请求模型。"""

from typing import List, Dict, Any, Optional
from pydantic import BaseModel, Field

from . import config


class GenerateRequest(BaseModel):
    prompt: str = ""
    width: int = 1024
    height: int = 1024
    workflow_json: str = "Z-Image.json"
    params: Dict[str, Any] = {}
    type: str = "zimage"
    client_id: str = ""
    convert_to_jpg: bool = False


class DeleteHistoryRequest(BaseModel):
    timestamp: float


class TokenRequest(BaseModel):
    token: str


class CloudGenRequest(BaseModel):
    prompt: str
    api_key: str = ""
    model: str = ""
    resolution: str = "1024x1024"
    type: str = "zimage"
    image_urls: List[str] = []
    loras: Optional[Any] = None
    client_id: Optional[str] = None


class CloudPollRequest(BaseModel):
    task_id: str
    api_key: str = ""
    client_id: Optional[str] = None


class AIReference(BaseModel):
    url: str = ""
    name: str = ""
    role: str = ""


class OnlineImageRequest(BaseModel):
    prompt: str = Field(min_length=1, max_length=config.ONLINE_IMAGE_PROMPT_MAX_LENGTH)
    provider_id: str = "comfly"
    model: str = ""
    size: str = "1024x1024"
    quality: str = "auto"
    reference_images: List[AIReference] = []


class CanvasVideoRequest(BaseModel):
    prompt: str = Field(min_length=1, max_length=config.VIDEO_PROMPT_MAX_LENGTH)
    provider_id: str = "comfly"
    model: str = "veo3-fast"
    duration: int = 5
    aspect_ratio: str = "16:9"
    resolution: str = ""
    size: str = ""
    images: List[AIReference] = []
    videos: List[str] = []
    enhance_prompt: bool = False
    enable_upsample: bool = False
    watermark: bool = False
    seed: Optional[int] = None
    camerafixed: bool = False
    return_last_frame: bool = False
    generate_audio: bool = False


class FirstPartyKeysPayload(BaseModel):
    openai_api_key: Optional[str] = None
    volcengine_ark_api_key: Optional[str] = None


class GptImage2Request(BaseModel):
    prompt: str = Field(min_length=1, max_length=config.ONLINE_IMAGE_PROMPT_MAX_LENGTH)
    size: str = "1024x1024"
    quality: str = "auto"
    count: int = 1
    reference_images: List[AIReference] = []


class SeedreamRequest(BaseModel):
    prompt: str = Field(min_length=1, max_length=config.ONLINE_IMAGE_PROMPT_MAX_LENGTH)
    model: str = "doubao-seedream-4-5-251128"
    size: str = "1024x1024"
    count: int = 1
    seed: Optional[int] = None
    watermark: bool = False
    reference_images: List[AIReference] = []


class SeedanceRequest(BaseModel):
    run_id: str = ""
    node_id: str = ""
    input_signature: str = ""
    prompt: str = Field(min_length=1, max_length=config.VIDEO_PROMPT_MAX_LENGTH)
    model: str = "doubao-seedance-2-0-fast-260128"
    duration: int = 5
    aspect_ratio: str = "16:9"
    resolution: str = "720p"
    seed: Optional[int] = None
    generate_audio: bool = True
    return_last_frame: bool = False
    reference_images: List[AIReference] = []
    reference_videos: List[AIReference] = []
    reference_audios: List[AIReference] = []


class SeedanceStatusRequest(BaseModel):
    task_ids: List[str] = []
    run_id: str = ""


class SeedanceTaskListRequest(BaseModel):
    run_id: str = ""
    model: str = ""
    status: str = "all"
    page_size: int = 10
    submitted_after: Optional[float] = None
    known_task_ids: List[str] = []


class SeedanceClaimRequest(BaseModel):
    run_id: str = ""
    node_id: str = ""
    task_id: str = ""
    model: str = ""
    input_signature: str = ""


class ApiProviderPayload(BaseModel):
    id: str = ""
    name: str = ""
    base_url: str = ""
    protocol: str = "openai"
    image_generation_endpoint: str = ""
    image_edit_endpoint: str = ""
    enabled: bool = True
    primary: bool = False
    image_models: List[str] = []
    chat_models: List[str] = []
    video_models: List[str] = []
    ms_loras: List[Dict[str, Any]] = []
    ms_defaults_version: int = 0
    api_key: Optional[str] = None
    clear_key: bool = False


class ChatRequest(BaseModel):
    conversation_id: str = ""
    message: str = Field(min_length=1, max_length=config.LLM_MESSAGE_MAX_LENGTH)
    model: str = ""
    image_model: str = ""
    mode: str = "chat"
    size: str = "1024x1024"
    quality: str = "auto"
    reference_images: List[AIReference] = []
    provider: str = "comfly"
    ms_model: str = ""


class MsGenerateRequest(BaseModel):
    prompt: str
    api_key: str = ""
    model: str = "black-forest-labs/FLUX.2-klein-9B"
    image_urls: List[str] = []
    width: int = 0
    height: int = 0
    size: str = ""
    loras: Optional[Any] = None
    client_id: Optional[str] = None


class CanvasLLMRequest(BaseModel):
    message: str = Field(min_length=1, max_length=config.LLM_MESSAGE_MAX_LENGTH)
    system_prompt: str = "You are a helpful assistant."
    model: str = ""
    messages: List[Dict[str, Any]] = []
    provider: str = "comfly"
    ms_model: str = ""
    images: List[str] = []   # 可以是 /output/*.png、/assets/*.png 本地路径 或 http(s) URL 或 data URL


class ConversationCreateRequest(BaseModel):
    title: str = "新对话"


class CanvasCreateRequest(BaseModel):
    title: str = "未命名画布"
    icon: str = "🧩"
    kind: str = "classic"


class CanvasSaveRequest(BaseModel):
    title: str = "未命名画布"
    icon: str = "🧩"
    nodes: List[Dict[str, Any]] = []
    connections: List[Dict[str, Any]] = []
    viewport: Dict[str, Any] = {}
    logs: List[Dict[str, Any]] = []
    settings: Dict[str, Any] = {}
    base_updated_at: Optional[int] = None
    client_id: Optional[str] = None


class CanvasAssetCheckRequest(BaseModel):
    urls: List[str] = []


class CanvasAssetDownloadRequest(BaseModel):
    urls: List[str] = []
    filename: str = "canvas-output-images.zip"


class TestConnectionPayload(BaseModel):
    base_url: str = ""
    api_key: str = ""
    provider_id: str = ""


class ComfyInstancesPayload(BaseModel):
    instances: List[str] = []


class WorkflowField(BaseModel):
    id: str
    node: str = ""
    input: str = ""
    name: str = ""
    type: str = "text"
    default: Any = None
    min: Optional[float] = None
    max: Optional[float] = None
    step: Optional[float] = None
    options: List[str] = []
    random_enabled: bool = False


class WorkflowConfig(BaseModel):
    title: str = ""
    fields: List[WorkflowField] = []
    mini_cards: Dict[str, Any] = {}


class WorkflowUploadRequest(BaseModel):
    name: str
    workflow: Dict[str, Any]


class WorkflowRunRequest(BaseModel):
    fields: Dict[str, Any] = {}
    config: WorkflowConfig
    client_id: str = ""
