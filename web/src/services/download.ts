import { registerPlugin } from "@capacitor/core";
import { appURL } from "./base";
import { e2eeService } from "./e2ee";
import { fetchProofProtectedBlob } from "./file";
import { getNativeBridge } from "./nativeBridge";
import { getApiBaseURL, isNativeShellRuntime } from "./runtime";

type DownloadFileParams = {
  rootId: string;
  path: string;
  name?: string;
};

type NativeDownloadPlugin = {
  download: (opts: { url: string; filename: string }) => Promise<{
    downloadId: number;
    filename: string;
    directory: string;
  }>;
};

const NativeDownload = registerPlugin<NativeDownloadPlugin>("NativeDownload");

type WindowWithNativeDownloadBridge = Window & {
  MindFSNativeDownload?: {
    download?: (url: string, filename: string) => string;
  };
};

function sanitizeDownloadName(path: string, name?: string): string {
  const candidate = String(name || path || "").trim();
  if (!candidate) {
    return "download";
  }
  const parts = candidate.replace(/\\/g, "/").split("/").filter(Boolean);
  return parts[parts.length - 1] || "download";
}

function buildDownloadURL(rootId: string, path: string): string {
  return appURL("/api/file", new URLSearchParams({
    raw: "1",
    root: rootId,
    path,
    download: "1",
  }));
}

function toAbsoluteDownloadURL(url: string): string {
  if (/^https?:\/\//i.test(url)) {
    return url;
  }

  const apiBaseURL = getApiBaseURL();
  if (apiBaseURL) {
    return new URL(url, `${apiBaseURL.replace(/\/+$/, "")}/`).toString();
  }

  if (typeof window !== "undefined" && /^https?:$/i.test(window.location.protocol)) {
    return new URL(url, window.location.href).toString();
  }

  return url;
}

function triggerBrowserDownload(url: string, filename: string): void {
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.rel = "noopener";
  anchor.style.display = "none";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
}

function triggerBrowserBlobDownload(blob: Blob, filename: string): void {
  const blobURL = URL.createObjectURL(blob);
  try {
    triggerBrowserDownload(blobURL, filename);
  } finally {
    window.setTimeout(() => URL.revokeObjectURL(blobURL), 60_000);
  }
}

async function downloadWithNativeShell(url: string, filename: string): Promise<void> {
  if (!/^https?:\/\//i.test(url)) {
    throw new Error("下载地址不是完整的 http/https URL，请先配置移动端 API 地址");
  }

  const unifiedBridge = getNativeBridge();
  if (typeof unifiedBridge?.download === "function") {
    const result = await unifiedBridge.download(JSON.stringify({ url, filename }));
    if (typeof result === "string" && result) {
      throw new Error(result);
    }
    return;
  }

  const nativeBridge = (window as WindowWithNativeDownloadBridge).MindFSNativeDownload;
  if (nativeBridge && typeof nativeBridge.download === "function") {
    const errorMessage = nativeBridge.download(url, filename);
    if (errorMessage) {
      throw new Error(errorMessage);
    }
    return;
  }

  await NativeDownload.download({ url, filename });
}

export async function downloadURL(url: string, filename = "download"): Promise<void> {
  if (typeof document === "undefined") {
    throw new Error("download is only available in browser runtime");
  }

  const safeFilename = sanitizeDownloadName(filename, filename);
  const absoluteURL = toAbsoluteDownloadURL(url);
  if (isNativeShellRuntime()) {
    await downloadWithNativeShell(absoluteURL, safeFilename);
    return;
  }

  triggerBrowserDownload(absoluteURL, safeFilename);
}

export async function downloadFile(params: DownloadFileParams): Promise<void> {
  const filename = sanitizeDownloadName(params.path, params.name);

  if (e2eeService.isRequired() && !isNativeShellRuntime()) {
    const blob = await fetchProofProtectedBlob({
      rootId: params.rootId,
      path: params.path,
    });
    triggerBrowserBlobDownload(blob, filename);
    return;
  }

  const url = toAbsoluteDownloadURL(buildDownloadURL(params.rootId, params.path));
  await downloadURL(url, filename);
}
