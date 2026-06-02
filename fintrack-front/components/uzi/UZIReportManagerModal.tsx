import React, { useEffect, useMemo, useState } from 'react';
import { useLanguage } from '../../contexts/LanguageContext';
import {
    UZIReportItem,
    WatchlistItem,
    normalizeUZITicker,
    uziAPI,
} from '../../services/apiService';

interface UZIReportManagerModalProps {
    isOpen: boolean;
    item: WatchlistItem | null;
    onClose: () => void;
    onReportsChanged?: () => Promise<void> | void;
    onAuthError?: () => void;
}

const depthOptions: Array<'lite' | 'medium' | 'deep'> = ['lite', 'medium', 'deep'];

const formatUpdatedAt = (value?: string) => {
    if (!value) {
        return '—';
    }
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) {
        return value;
    }
    return parsed.toLocaleString('zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
    });
};

const formatBytes = (value?: number) => {
    if (!value) {
        return '—';
    }
    if (value >= 1024 * 1024) {
        return `${(value / (1024 * 1024)).toFixed(1)} MB`;
    }
    if (value >= 1024) {
        return `${Math.round(value / 1024)} KB`;
    }
    return `${value} B`;
};

const UZIReportManagerModal: React.FC<UZIReportManagerModalProps> = ({
    isOpen,
    item,
    onClose,
    onReportsChanged,
    onAuthError,
}) => {
    const { t, language } = useLanguage();
    const isZh = language === 'zh';
    const copy = {
        submitted: isZh ? '已提交生成请求，正在初始化 UZI 运行环境' : 'Generation request submitted. Initializing UZI runtime.',
        initializing: isZh ? '正在初始化' : 'Initializing',
        stage: isZh ? '当前阶段' : 'Current stage',
        failed: isZh ? '研报生成失败' : 'Report generation failed',
        completed: isZh ? '研报生成完成，可直接查看' : 'Report generation completed. You can open it now.',
    };
    const [reports, setReports] = useState<UZIReportItem[]>([]);
    const [isLoading, setIsLoading] = useState(false);
    const [isGenerating, setIsGenerating] = useState(false);
    const [deletingPath, setDeletingPath] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [selectedDepth, setSelectedDepth] = useState<'lite' | 'medium' | 'deep'>('medium');
    const [streamSummary, setStreamSummary] = useState<string | null>(null);

    const symbol = item?.stock?.symbol || '';
    const companyName = item?.stock?.company_name || symbol;
    const uziTicker = useMemo(() => normalizeUZITicker(symbol), [symbol]);

    const handleAPIError = (message: string) => {
        if (onAuthError && (
            message.includes('Authorization header required') ||
            message.includes('401') ||
            message.includes('Unauthorized')
        )) {
            onAuthError();
            return true;
        }
        return false;
    };

    const loadReports = async () => {
        if (!uziTicker) {
            setReports([]);
            return;
        }
        try {
            setIsLoading(true);
            setError(null);
            const response = await uziAPI.listReports(uziTicker);
            setReports(response.items || []);
        } catch (err: any) {
            const message = err?.message || 'Failed to load UZI reports';
            if (handleAPIError(message)) {
                return;
            }
            setError(message);
        } finally {
            setIsLoading(false);
        }
    };

    useEffect(() => {
        if (!isOpen) {
            return;
        }
        setSelectedDepth('medium');
        void loadReports();
    }, [isOpen, uziTicker]);

    if (!isOpen || !item) {
        return null;
    }

    const handleGenerate = async () => {
        if (!uziTicker) {
            return;
        }
        try {
            setIsGenerating(true);
            setError(null);
            setStreamSummary(copy.submitted);
            await uziAPI.analyzeStream({
                ticker: uziTicker,
                depth: selectedDepth,
                no_resume: false,
            }, {
                onStart: event => setStreamSummary(event.data.summary || copy.initializing),
                onStage: event => setStreamSummary(event.data.summary || `${copy.stage}: ${event.data.stage}`),
                onError: event => setStreamSummary(event.data.error || copy.failed),
                onComplete: () => setStreamSummary(copy.completed),
            });
            await loadReports();
            await onReportsChanged?.();
        } catch (err: any) {
            const message = err?.message || 'Failed to generate UZI report';
            if (handleAPIError(message)) {
                return;
            }
            setError(message);
        } finally {
            setIsGenerating(false);
        }
    };

    const handleDelete = async (report: UZIReportItem) => {
        try {
            setDeletingPath(report.report_relative_path);
            setError(null);
            await uziAPI.deleteReport(report.report_relative_path);
            await loadReports();
            await onReportsChanged?.();
        } catch (err: any) {
            const message = err?.message || 'Failed to delete UZI report';
            if (handleAPIError(message)) {
                return;
            }
            setError(message);
        } finally {
            setDeletingPath(null);
        }
    };

    const handleOpenReport = async (report: UZIReportItem) => {
        try {
            await uziAPI.openReport(report.report_relative_path);
        } catch (err: any) {
            const message = err?.message || 'Failed to open UZI report';
            if (handleAPIError(message)) {
                return;
            }
            setError(message);
        }
    };

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm">
            <div className="flex max-h-[90vh] w-full max-w-4xl flex-col overflow-hidden rounded-2xl border border-white/10 bg-card-dark shadow-2xl">
                <div className="flex items-start justify-between border-b border-white/10 px-6 py-5">
                    <div>
                        <h2 className="text-xl font-bold text-white">{t('watchlist.uziManage', 'UZI 管理')}</h2>
                        <p className="mt-1 text-sm text-white/60">
                            {companyName} · {uziTicker}
                        </p>
                    </div>
                    <button
                        onClick={onClose}
                        className="rounded-full p-2 text-white/60 transition-colors hover:bg-white/10 hover:text-white"
                    >
                        <span className="material-symbols-outlined">close</span>
                    </button>
                </div>

                <div className="flex-1 space-y-5 overflow-y-auto px-6 py-6">
                    <div className="rounded-2xl border border-white/10 bg-white/[0.03] p-4">
                        <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
                            <div className="flex-1">
                                <p className="text-sm font-medium text-white/80">{t('uzi.generateDepth', '生成深度')}</p>
                                <div className="mt-3 flex flex-wrap gap-2">
                                    {depthOptions.map(option => (
                                        <button
                                            key={option}
                                            type="button"
                                            onClick={() => setSelectedDepth(option)}
                                            disabled={isGenerating}
                                            className={`rounded-lg px-4 py-2 text-sm font-medium transition-colors ${
                                                selectedDepth === option
                                                    ? 'bg-primary text-black'
                                                    : 'bg-white/5 text-white/70 hover:bg-white/10 hover:text-white'
                                            }`}
                                        >
                                            {option}
                                        </button>
                                    ))}
                                </div>
                            </div>
                            <div className="flex flex-wrap gap-2">
                                <button
                                    type="button"
                                    onClick={() => void loadReports()}
                                    disabled={isLoading || isGenerating}
                                    className="rounded-lg border border-white/10 bg-white/5 px-4 py-2 text-sm font-medium text-white/80 transition-colors hover:bg-white/10 hover:text-white disabled:opacity-50"
                                >
                                    {t('uzi.query', '查询')}
                                </button>
                                <button
                                    type="button"
                                    onClick={() => void handleGenerate()}
                                    disabled={isGenerating}
                                    className="rounded-lg bg-primary px-4 py-2 text-sm font-bold text-black transition-opacity hover:opacity-90 disabled:opacity-50"
                                >
                                    {isGenerating ? t('uzi.generating', '生成中...') : t('uzi.generate', '生成')}
                                </button>
                            </div>
                        </div>
                    </div>

                    {error && (
                        <div className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-300">
                            {error}
                        </div>
                    )}

                    {streamSummary && (
                        <div className="rounded-xl border border-primary/20 bg-primary/10 px-4 py-3 text-sm text-white/85">
                            {streamSummary}
                        </div>
                    )}

                    <div className="rounded-2xl border border-white/10 bg-white/[0.02]">
                        <div className="flex items-center justify-between border-b border-white/10 px-4 py-3">
                            <div>
                                <p className="text-sm font-semibold text-white">{t('uzi.reportList', '报告列表')}</p>
                                <p className="text-xs text-white/45">
                                    {reports.length > 0
                                        ? `${reports.length} ${t('uzi.reportCount', '份报告')}`
                                        : t('uzi.emptyHint', '当前还没有生成过 UZI 报告')}
                                </p>
                            </div>
                        </div>

                        <div className="divide-y divide-white/10">
                            {isLoading ? (
                                <div className="flex items-center justify-center px-6 py-12 text-white/55">
                                    <div className="h-5 w-5 animate-spin rounded-full border-2 border-white/20 border-t-primary"></div>
                                </div>
                            ) : reports.length === 0 ? (
                                <div className="px-6 py-12 text-center text-sm text-white/45">
                                    {t('uzi.empty', '暂无 UZI 报告，点击上方生成即可创建。')}
                                </div>
                            ) : (
                                reports.map(report => (
                                    <div
                                        key={report.report_relative_path}
                                        className="flex flex-col gap-4 px-4 py-4 lg:flex-row lg:items-center lg:justify-between"
                                    >
                                        <div className="min-w-0">
                                            <div className="flex flex-wrap items-center gap-2">
                                                <span className="rounded-full bg-primary/15 px-2.5 py-1 text-xs font-semibold text-primary">
                                                    {report.date_tag || report.directory_name}
                                                </span>
                                                <span className="text-xs text-white/40">{formatBytes(report.size_bytes)}</span>
                                            </div>
                                            <p className="mt-2 truncate text-sm font-medium text-white/85">
                                                {report.report_relative_path}
                                            </p>
                                            <p className="mt-1 text-xs text-white/45">
                                                {t('uzi.updatedAt', '更新时间')} · {formatUpdatedAt(report.updated_at)}
                                            </p>
                                        </div>

                                        <div className="flex shrink-0 flex-wrap gap-2">
                                            <button
                                                type="button"
                                                onClick={() => void handleOpenReport(report)}
                                                className="rounded-lg border border-white/10 bg-white/5 px-4 py-2 text-sm font-medium text-white/80 transition-colors hover:bg-white/10 hover:text-white"
                                            >
                                                {t('uzi.open', '查看')}
                                            </button>
                                            <button
                                                type="button"
                                                onClick={() => void handleDelete(report)}
                                                disabled={deletingPath === report.report_relative_path}
                                                className="rounded-lg border border-red-500/20 bg-red-500/10 px-4 py-2 text-sm font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                                            >
                                                {deletingPath === report.report_relative_path
                                                    ? t('uzi.deleting', '删除中...')
                                                    : t('uzi.delete', '删除')}
                                            </button>
                                        </div>
                                    </div>
                                ))
                            )}
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
};

export default UZIReportManagerModal;
