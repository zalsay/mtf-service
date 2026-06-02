import React, { useEffect, useMemo, useState } from 'react';
import { useLanguage } from '../../contexts/LanguageContext';
import {
    UZIReportItem,
    normalizeUZITicker,
    uziAPI,
} from '../../services/apiService';

interface UZIReportsProps {
    onAuthError?: () => void;
}

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

const UZIReports: React.FC<UZIReportsProps> = ({ onAuthError }) => {
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
    const [searchTerm, setSearchTerm] = useState('');
    const [generateTicker, setGenerateTicker] = useState('');
    const [selectedDepth, setSelectedDepth] = useState<'lite' | 'medium' | 'deep'>('medium');
    const [isLoading, setIsLoading] = useState(false);
    const [isGenerating, setIsGenerating] = useState(false);
    const [deletingPath, setDeletingPath] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [streamSummary, setStreamSummary] = useState<string | null>(null);

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
        try {
            setIsLoading(true);
            setError(null);
            const response = await uziAPI.listReports();
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
        void loadReports();
    }, []);

    const filteredReports = useMemo(() => {
        const keyword = searchTerm.trim().toLowerCase();
        if (!keyword) {
            return reports;
        }
        return reports.filter(report => (
            report.ticker.toLowerCase().includes(keyword)
            || report.directory_name.toLowerCase().includes(keyword)
            || report.report_relative_path.toLowerCase().includes(keyword)
        ));
    }, [reports, searchTerm]);

    const handleGenerate = async (event: React.FormEvent) => {
        event.preventDefault();
        const normalizedTicker = normalizeUZITicker(generateTicker);
        if (!normalizedTicker) {
            setError(t('uzi.invalidTicker', '请输入有效的股票代码，例如 601766.SH'));
            return;
        }
        try {
            setIsGenerating(true);
            setError(null);
            setStreamSummary(copy.submitted);
            await uziAPI.analyzeStream({
                ticker: normalizedTicker,
                depth: selectedDepth,
                no_resume: false,
            }, {
                onStart: event => setStreamSummary(event.data.summary || copy.initializing),
                onStage: event => setStreamSummary(event.data.summary || `${copy.stage}: ${event.data.stage}`),
                onError: event => setStreamSummary(event.data.error || copy.failed),
                onComplete: () => setStreamSummary(copy.completed),
            });
            setGenerateTicker(normalizedTicker);
            await loadReports();
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

    const handleOpen = async (report: UZIReportItem) => {
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
        <div className="flex w-full flex-col">
            <div className="mb-4 flex flex-wrap justify-between gap-3 p-4">
                <div className="flex min-w-72 flex-col gap-2">
                    <p className="text-4xl font-black leading-tight tracking-[-0.033em] text-white">{t('uzi.title', '研报Skill')}</p>
                    <p className="text-base font-normal leading-normal text-white/60">
                        {t('uzi.subtitle', '统一管理 UZI 研报的查询、生成与删除。')}
                    </p>
                </div>
            </div>

            <div className="space-y-4 px-4">
                <form
                    onSubmit={handleGenerate}
                    className="rounded-2xl border border-white/10 bg-white/[0.03] p-4"
                >
                    <div className="flex flex-col gap-4 xl:flex-row xl:items-end">
                        <div className="flex-1">
                            <label className="block text-sm font-medium text-white/80">{t('uzi.ticker', '股票代码')}</label>
                            <input
                                value={generateTicker}
                                onChange={event => setGenerateTicker(event.target.value)}
                                placeholder={t('uzi.tickerPlaceholder', '例如 601766.SH 或 sh601766')}
                                className="mt-2 h-12 w-full rounded-xl border border-white/10 bg-black/20 px-4 text-sm text-white outline-none transition-colors focus:border-primary"
                            />
                        </div>
                        <div>
                            <label className="block text-sm font-medium text-white/80">{t('uzi.generateDepth', '生成深度')}</label>
                            <div className="mt-2 flex gap-2">
                                {(['lite', 'medium', 'deep'] as const).map(option => (
                                    <button
                                        key={option}
                                        type="button"
                                        onClick={() => setSelectedDepth(option)}
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
                        <div className="flex gap-2">
                            <button
                                type="button"
                                onClick={() => void loadReports()}
                                disabled={isLoading || isGenerating}
                                className="h-12 rounded-xl border border-white/10 bg-white/5 px-4 text-sm font-medium text-white/80 transition-colors hover:bg-white/10 hover:text-white disabled:opacity-50"
                            >
                                {t('uzi.refresh', '刷新')}
                            </button>
                            <button
                                type="submit"
                                disabled={isGenerating}
                                className="h-12 rounded-xl bg-primary px-5 text-sm font-bold text-black transition-opacity hover:opacity-90 disabled:opacity-50"
                            >
                                {isGenerating ? t('uzi.generating', '生成中...') : t('uzi.generate', '生成')}
                            </button>
                        </div>
                    </div>
                </form>

                {streamSummary ? (
                    <div className="rounded-xl border border-primary/20 bg-primary/10 px-4 py-3 text-sm text-white/85">
                        {streamSummary}
                    </div>
                ) : null}

                <div className="rounded-2xl border border-white/10 bg-white/[0.02] p-4">
                    <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
                        <div>
                            <p className="text-sm font-semibold text-white">{t('uzi.reportList', '报告列表')}</p>
                            <p className="mt-1 text-xs text-white/45">
                                {filteredReports.length} / {reports.length} {t('uzi.reportCount', '份报告')}
                            </p>
                        </div>
                        <div className="flex h-12 w-full items-stretch rounded-xl border border-white/10 bg-black/20 lg:max-w-sm">
                            <div className="flex items-center pl-4 text-white/45">
                                <span className="material-symbols-outlined">search</span>
                            </div>
                            <input
                                value={searchTerm}
                                onChange={event => setSearchTerm(event.target.value)}
                                placeholder={t('uzi.searchPlaceholder', '搜索代码或目录')}
                                className="h-full w-full rounded-xl bg-transparent px-4 text-sm text-white outline-none placeholder:text-white/30"
                            />
                        </div>
                    </div>
                </div>

                {error && (
                    <div className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-300">
                        {error}
                    </div>
                )}

                <div className="space-y-3">
                    {isLoading ? (
                        <div className="flex items-center justify-center rounded-2xl border border-white/10 bg-white/[0.02] px-6 py-16 text-white/50">
                            <div className="h-6 w-6 animate-spin rounded-full border-2 border-white/20 border-t-primary"></div>
                        </div>
                    ) : filteredReports.length === 0 ? (
                        <div className="rounded-2xl border border-white/10 bg-white/[0.02] px-6 py-16 text-center text-sm text-white/45">
                            {t('uzi.empty', '暂无 UZI 报告，先生成一份试试。')}
                        </div>
                    ) : (
                        filteredReports.map(report => (
                            <div
                                key={report.report_relative_path}
                                className="flex flex-col gap-4 rounded-2xl border border-white/10 bg-white/[0.02] p-4 lg:flex-row lg:items-center lg:justify-between"
                            >
                                <div className="min-w-0">
                                    <div className="flex flex-wrap items-center gap-2">
                                        <span className="rounded-full bg-primary/15 px-2.5 py-1 text-xs font-semibold text-primary">
                                            {report.ticker}
                                        </span>
                                        <span className="rounded-full bg-white/6 px-2.5 py-1 text-xs text-white/55">
                                            {report.date_tag || report.directory_name}
                                        </span>
                                    </div>
                                    <p className="mt-2 truncate text-sm font-medium text-white/85">{report.report_relative_path}</p>
                                    <p className="mt-1 text-xs text-white/45">
                                        {formatUpdatedAt(report.updated_at)} · {formatBytes(report.size_bytes)}
                                    </p>
                                </div>

                                <div className="flex shrink-0 flex-wrap gap-2">
                                    <button
                                        type="button"
                                        onClick={() => void handleOpen(report)}
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
    );
};

export default UZIReports;
