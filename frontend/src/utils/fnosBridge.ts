import { TrimApp } from '@trimjs/web-app';

let sdkInstance: TrimApp | null = null;
let readyPromise: Promise<TrimApp | null> | null = null;

async function getTrimApp(): Promise<TrimApp | null> {
  if (typeof window === 'undefined') return null;
  if (!sdkInstance) {
    sdkInstance = new TrimApp();
  }
  if (!readyPromise) {
    readyPromise = sdkInstance
      .ready()
      .then(() => sdkInstance)
      .catch(() => sdkInstance);
  }
  return readyPromise;
}

export interface ParsedLocalFileLink {
  path: string;
  isDir: boolean;
  lineRange?: string;
}

/**
 * Parses `file:///vol1/...` (with optional `#L10-L20` fragment) or direct `/vol*` NAS paths.
 */
export function extractLocalFilePath(href?: string): ParsedLocalFileLink | null {
  if (!href) return null;
  const trimmed = href.trim();
  if (!trimmed) return null;

  let rawPath = '';
  let lineRange: string | undefined;

  if (/^file:\/\//i.test(trimmed)) {
    let rest = trimmed.replace(/^file:\/\//i, '');
    // Strip optional `localhost` authority: file://localhost/vol1/... -> /vol1/...
    if (/^localhost\//i.test(rest)) {
      rest = rest.slice('localhost'.length);
    }
    const hashIdx = rest.indexOf('#');
    if (hashIdx >= 0) {
      const frag = rest.slice(hashIdx + 1);
      if (frag) lineRange = frag;
      rest = rest.slice(0, hashIdx);
    }
    const qIdx = rest.indexOf('?');
    if (qIdx >= 0) {
      rest = rest.slice(0, qIdx);
    }
    try {
      rawPath = decodeURIComponent(rest);
    } catch {
      rawPath = rest;
    }
  } else if (/^\/(vol\d+|share|home|mnt|media|data|var|etc|tmp)\b/i.test(trimmed)) {
    const hashIdx = trimmed.indexOf('#');
    if (hashIdx >= 0) {
      const frag = trimmed.slice(hashIdx + 1);
      if (frag) lineRange = frag;
      rawPath = trimmed.slice(0, hashIdx);
    } else {
      rawPath = trimmed;
    }
    try {
      rawPath = decodeURIComponent(rawPath);
    } catch {
      // keep rawPath
    }
  } else {
    return null;
  }

  if (!rawPath.startsWith('/')) return null;

  const isDir = rawPath.endsWith('/') && rawPath.length > 1;
  const cleanPath = isDir ? rawPath.replace(/\/+$/, '') : rawPath;
  return {
    path: cleanPath || '/',
    isDir,
    lineRange,
  };
}

function getParentDir(path: string): string {
  const clean = path.replace(/\/+$/, '');
  const idx = clean.lastIndexOf('/');
  if (idx <= 0) return '/';
  return clean.slice(0, idx);
}

async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // fallback below
  }
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(ta);
    return ok;
  } catch {
    return false;
  }
}

export interface OpenFileResult {
  ok: boolean;
  mode: 'file' | 'folder' | 'copied' | 'error';
  message: string;
}

/**
 * Opens a NAS file (or directory) directly in the fnOS host via `@trimjs/web-app`.
 * Falls back to opening its parent folder in fnOS File Manager, or copying the path
 * when accessed from a standalone browser outside fnOS desktop / App.
 */
export async function openPathInFnOS(
  filePath: string,
  options?: { isDir?: boolean; preferFolder?: boolean }
): Promise<OpenFileResult> {
  const cleanPath = filePath.replace(/\/+$/, '') || '/';
  const app = await getTrimApp();

  if (app && app.isStandaloneWeb && window.parent !== window) {
    try {
      await app.reconnectHost();
    } catch {
      // ignore reconnect failure
    }
  }

  if (!app || app.isStandaloneWeb) {
    const copied = await copyToClipboard(cleanPath);
    return {
      ok: false,
      mode: copied ? 'copied' : 'error',
      message: copied
        ? `未检测到飞牛宿主桥（需在飞牛桌面/App 内打开，且 fnOS ≥ 1.2.0401）；已复制路径：${cleanPath}`
        : `请在飞牛桌面或飞牛 App 内打开以直接预览文件：${cleanPath}`,
    };
  }

  const openFolderOnly = Boolean(options?.isDir || options?.preferFolder);
  if (openFolderOnly) {
    const dir = options?.isDir ? cleanPath : getParentDir(cleanPath);
    try {
      await app.openFileManager(dir);
      return {
        ok: true,
        mode: 'folder',
        message: `已在飞牛文件管理中打开目录：${dir}`,
      };
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err);
      console.warn('[fnosBridge] openFileManager failed:', reason);
      const copied = await copyToClipboard(dir);
      return {
        ok: false,
        mode: copied ? 'copied' : 'error',
        message: copied
          ? `飞牛宿主打开目录失败（${reason}），已复制路径：${dir}`
          : `无法唤起飞牛文件管理（${reason}）：${dir}`,
      };
    }
  }

  // Try opening the file directly in fnOS first.
  try {
    await app.openFile(cleanPath);
    return {
      ok: true,
      mode: 'file',
      message: `已在飞牛系统中打开：${cleanPath.split('/').pop() || cleanPath}`,
    };
  } catch (fileErr) {
    const fileReason = fileErr instanceof Error ? fileErr.message : String(fileErr);
    console.warn('[fnosBridge] openFile failed, trying openFileManager:', fileReason);
    // Fallback: open containing folder in fnOS File Manager.
    const parentDir = getParentDir(cleanPath);
    try {
      await app.openFileManager(parentDir);
      return {
        ok: true,
        mode: 'folder',
        message: `无法直接预览该文件，已打开所在目录：${parentDir}`,
      };
    } catch (dirErr) {
      const dirReason = dirErr instanceof Error ? dirErr.message : String(dirErr);
      console.warn('[fnosBridge] openFileManager fallback failed:', dirReason);
      const copied = await copyToClipboard(cleanPath);
      return {
        ok: false,
        mode: copied ? 'copied' : 'error',
        message: copied
          ? `飞牛宿主暂不支持打开（${fileReason}），已复制路径：${cleanPath}`
          : `无法在飞牛中打开该文件（${fileReason}）：${cleanPath}`,
      };
    }
  }
}
