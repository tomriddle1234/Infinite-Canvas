"""Routes for browsing existing Volcengine Ark real-person portrait assets."""

from typing import List, Optional

from fastapi import APIRouter, HTTPException, Query
from fastapi.responses import RedirectResponse
from pydantic import BaseModel

from .. import upstream_volcengine_assets as assets

router = APIRouter()


class AssetGroupsRequest(BaseModel):
    group_type: str = "LivenessFace"
    query: str = ""
    page: int = 1
    page_size: int = 20


class AssetsListRequest(BaseModel):
    group_type: str = "LivenessFace"
    group_id: str = ""
    query: str = ""
    statuses: List[str] = ["Active"]
    page: int = 1
    page_size: int = 24


class AssetDetailRequest(BaseModel):
    asset_id: str


class PresetPortraitSearchRequest(BaseModel):
    query: str = ""
    page: int = 1
    page_size: int = 24


@router.post("/api/volcengine/assets/groups")
async def volcengine_asset_groups(payload: AssetGroupsRequest):
    return await assets.list_asset_groups(
        group_type=payload.group_type,
        query=payload.query,
        page=payload.page,
        page_size=payload.page_size,
    )


@router.post("/api/volcengine/assets/list")
async def volcengine_assets_list(payload: AssetsListRequest):
    return await assets.list_assets(
        group_type=payload.group_type,
        group_id=payload.group_id,
        query=payload.query,
        statuses=payload.statuses,
        page=payload.page,
        page_size=payload.page_size,
    )


@router.post("/api/volcengine/assets/detail")
async def volcengine_asset_detail(payload: AssetDetailRequest):
    return await assets.get_asset(payload.asset_id)


@router.post("/api/volcengine/preset-portraits/search")
async def volcengine_preset_portraits_search(payload: PresetPortraitSearchRequest):
    return await assets.search_preset_virtual_portraits(
        query=payload.query,
        page=payload.page,
        page_size=payload.page_size,
    )


@router.get("/api/volcengine/assets/preview/{asset_id}")
async def volcengine_asset_preview(asset_id: str, refresh: Optional[bool] = Query(False)):
    url = await assets.cached_asset_preview(asset_id, refresh=bool(refresh))
    if not url:
        raise HTTPException(status_code=404, detail="素材预览图暂不可用")
    return RedirectResponse(url=url)
