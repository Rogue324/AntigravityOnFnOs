import React, { useEffect, useState } from 'react';
import { Folder, FolderOpen, HardDrive, ArrowUp, X, Check, Eye, EyeOff } from 'lucide-react';
import { api } from '../api/client';
import type { BrowseResponse } from '../types';

interface DirPickerModalProps {
  open: boolean;
  title?: string;
  initialPath?: string;
  onSelect: (path: string) => void;
  onClose: () => void;
}

export const DirPickerModal: React.FC<DirPickerModalProps> = ({
  open,
  title = '选择 NAS 工作区目录',
  initialPath = '',
  onSelect,
  onClose,
}) => {
  const [data, setData] = useState<BrowseResponse | null>(null);
  const [manualPath, setManualPath] = useState(initialPath);
  const [showHidden, setShowHidden] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const loadDir = async (targetPath: string, hidden = showHidden) => {
    setLoading(true);
    setError('');
    try {
      const resp = await api.browseFS(targetPath, hidden);
      setData(resp);
      setManualPath(resp.path);
    } catch (err) {
      setError(err instanceof Error ? err.message : '无法读取该目录');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (open) {
      loadDir(initialPath, showHidden);
    }
  }, [open]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm dark:bg-black/70">
      <div className="flex max-h-[82vh] w-full max-w-2xl flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:bg-slate-900">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-slate-200 px-5 py-3.5 dark:border-slate-800">
          <div className="flex items-center gap-2">
            <FolderOpen className="h-5 w-5 text-indigo-500 dark:text-indigo-400" />
            <h3 className="text-sm font-semibold text-slate-900 dark:text-white">{title}</h3>
          </div>
          <button
            onClick={onClose}
            className="rounded-lg p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-700 dark:hover:bg-slate-800 dark:hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Roots quick bar */}
        {data && data.roots.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5 border-b border-slate-200 bg-slate-50 px-5 py-2.5 dark:border-slate-800 dark:bg-slate-950/60">
            {data.roots.map((root) => (
              <button
                key={root.path}
                onClick={() => loadDir(root.path)}
                className={`flex items-center gap-1.5 rounded-lg border px-2.5 py-1 text-xs transition ${
                  data.path === root.path
                    ? 'border-indigo-500/60 bg-indigo-50 text-indigo-700 dark:bg-indigo-500/20 dark:text-indigo-200'
                    : 'border-slate-200 bg-white text-slate-700 hover:border-slate-300 dark:border-slate-700/80 dark:bg-slate-900 dark:text-slate-300 dark:hover:border-slate-600 dark:hover:text-white'
                }`}
              >
                <HardDrive className="h-3.5 w-3.5 text-slate-400" />
                <span>{root.label}</span>
              </button>
            ))}
          </div>
        )}

        {/* Path input & controls */}
        <div className="flex items-center gap-2 border-b border-slate-200 px-5 py-2.5 dark:border-slate-800">
          <button
            onClick={() => data?.parent && loadDir(data.parent)}
            disabled={!data?.parent || loading}
            title="上级目录"
            className="rounded-lg border border-slate-200 bg-slate-50 p-1.5 text-slate-600 hover:bg-slate-100 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-800/70 dark:text-slate-300 dark:hover:bg-slate-800"
          >
            <ArrowUp className="h-4 w-4" />
          </button>
          <input
            type="text"
            value={manualPath}
            onChange={(e) => setManualPath(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') loadDir(manualPath);
            }}
            className="flex-1 rounded-lg border border-slate-200 bg-slate-50 px-3 py-1.5 font-mono text-xs text-slate-800 focus:border-indigo-500 focus:bg-white focus:outline-none dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"
          />
          <button
            onClick={() => loadDir(manualPath)}
            className="rounded-lg border border-slate-200 bg-slate-100 px-3 py-1.5 text-xs text-slate-700 hover:bg-slate-200 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700"
          >
            前往
          </button>
          <button
            onClick={() => {
              const next = !showHidden;
              setShowHidden(next);
              loadDir(data?.path || manualPath, next);
            }}
            title={showHidden ? '隐藏点目录' : '显示隐藏目录'}
            className="rounded-lg border border-slate-200 bg-slate-50 p-1.5 text-slate-600 hover:bg-slate-100 dark:border-slate-700 dark:bg-slate-800/70 dark:text-slate-300 dark:hover:bg-slate-800"
          >
            {showHidden ? <Eye className="h-4 w-4 text-indigo-500 dark:text-indigo-400" /> : <EyeOff className="h-4 w-4" />}
          </button>
        </div>

        {/* Directory listing */}
        <div className="min-h-[240px] flex-1 overflow-y-auto p-3">
          {error && (
            <div className="mb-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-600 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-300">
              {error}
            </div>
          )}

          {loading ? (
            <div className="py-12 text-center text-xs text-slate-400">正在读取目录...</div>
          ) : data && data.entries.length > 0 ? (
            <div className="grid grid-cols-1 gap-1 sm:grid-cols-2">
              {data.entries.map((entry) => (
                <button
                  key={entry.path}
                  onClick={() => loadDir(entry.path)}
                  className="flex items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm text-slate-700 transition hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800"
                >
                  <Folder className="h-4 w-4 shrink-0 text-amber-500 dark:text-amber-400/90" />
                  <span className="truncate">{entry.name}</span>
                </button>
              ))}
            </div>
          ) : (
            <div className="py-12 text-center text-xs text-slate-400 dark:text-slate-500">
              当前目录下没有子文件夹
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex flex-col gap-2 border-t border-slate-200 bg-slate-50 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5 dark:border-slate-800 dark:bg-slate-950/60">
          <div className="truncate font-mono text-xs text-slate-500 dark:text-slate-400">
            当前选中：<span className="text-slate-800 dark:text-slate-200">{data?.path || manualPath}</span>
          </div>
          <div className="flex items-center justify-end gap-2">
            <button
              onClick={onClose}
              className="rounded-lg border border-slate-200 bg-white px-3.5 py-1.5 text-xs text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:bg-transparent dark:text-slate-300 dark:hover:bg-slate-800"
            >
              取消
            </button>
            <button
              onClick={() => {
                const chosen = data?.path || manualPath;
                if (chosen) onSelect(chosen);
              }}
              className="flex items-center gap-1.5 rounded-lg bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500"
            >
              <Check className="h-3.5 w-3.5" />
              <span>确认选择</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
