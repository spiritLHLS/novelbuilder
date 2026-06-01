from __future__ import annotations

import os
import platform
import shutil
import subprocess
from dataclasses import asdict, dataclass


@dataclass
class AcceleratorInfo:
    kind: str
    available: bool
    name: str = ""
    reason: str = ""


def _cmd_available(name: str) -> bool:
    return shutil.which(name) is not None


def _first_line(cmd: list[str], timeout: float = 2.0) -> str:
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout, check=False)
    except Exception:
        return ""
    output = (result.stdout or result.stderr or "").strip()
    return output.splitlines()[0] if output else ""


def detect_accelerators() -> dict:
    preferred = os.getenv("NB_ACCELERATOR", "auto").strip().lower()
    accelerators: list[AcceleratorInfo] = []

    cuda_name = ""
    if _cmd_available("nvidia-smi"):
        cuda_name = _first_line([
            "nvidia-smi",
            "--query-gpu=name",
            "--format=csv,noheader",
        ])
    accelerators.append(AcceleratorInfo(
        kind="cuda",
        available=bool(cuda_name),
        name=cuda_name,
        reason="" if cuda_name else "nvidia-smi not found or no CUDA GPU visible",
    ))

    rocm_name = ""
    if _cmd_available("rocminfo"):
        rocm_name = _first_line(["rocminfo"])
    accelerators.append(AcceleratorInfo(
        kind="rocm",
        available=bool(rocm_name),
        name=rocm_name[:120],
        reason="" if rocm_name else "rocminfo not found or no ROCm device visible",
    ))

    npu_name = ""
    for cmd in ("vainfo", "npu-smi", "xpu-smi"):
        if _cmd_available(cmd):
            npu_name = _first_line([cmd])
            if npu_name:
                break
    accelerators.append(AcceleratorInfo(
        kind="npu",
        available=bool(npu_name),
        name=npu_name[:120],
        reason="" if npu_name else "no known NPU command detected",
    ))

    selected = "cpu"
    if preferred != "cpu":
        for item in accelerators:
            if item.available and (preferred in ("auto", item.kind)):
                selected = item.kind
                break

    return {
        "platform": platform.platform(),
        "machine": platform.machine(),
        "preferred": preferred,
        "selected": selected,
        "selected_accelerator": selected,
        "accelerators": [asdict(item) for item in accelerators],
    }
