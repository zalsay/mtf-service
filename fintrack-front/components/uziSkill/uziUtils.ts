import { UZIAnalyzeResponse, UZIReportItem } from '../../services/apiService';
import type { Language } from '../../contexts/LanguageContext';

export const formatUpdatedAt = (value?: string) => {
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

export const formatBytes = (value?: number) => {
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

export const isUZIAuthError = (message: string) => (
    message.includes('Authorization header required')
    || message.includes('401')
    || message.includes('Unauthorized')
);

export const getDepthMeta = (value: 'medium' | 'deep', language: Language = 'zh') => {
    const isZh = language === 'zh';
    if (value === 'deep') {
        return {
            label: isZh ? '深入' : 'Deep',
            hint: isZh ? '完整 UZI 深度研报' : 'Full-depth UZI report',
        };
    }
    return {
        label: isZh ? '标准' : 'Standard',
        hint: isZh ? '轻量 AI 研报' : 'Lightweight AI report',
    };
};

export const uziDepthOptions = ['medium', 'deep'] as const;

export type UZIDepth = typeof uziDepthOptions[number];

export const normalizeReportDepth = (value?: string | null): UZIDepth => {
    if (value === 'deep') {
        return 'deep';
    }
    return 'medium';
};

export const getAnalyzeSummary = (result: UZIAnalyzeResponse | null, language: Language = 'zh') => {
    if (!result) {
        return language === 'zh' ? '暂无' : 'None';
    }

    const ticker = result.report?.ticker || result.ticker || (language === 'zh' ? '未知代码' : 'Unknown');
    return ticker;
};

export const getChinaTodayDateTag = () => {
    const parts = new Intl.DateTimeFormat('en-US', {
        timeZone: 'Asia/Shanghai',
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
    }).formatToParts(new Date());
    const getPart = (type: string) => parts.find(part => part.type === type)?.value || '';
    return `${getPart('year')}${getPart('month')}${getPart('day')}`;
};

export const getTodayReport = (reports?: UZIReportItem[], depth?: UZIDepth) => {
    const todayDateTag = getChinaTodayDateTag();
    return (reports || []).find(report => {
        const matchesDate = report.date_tag === todayDateTag;
        const matchesDepth = !depth || normalizeReportDepth(report.depth) === depth;
        return matchesDate && matchesDepth;
    });
};

export const getLatestReportByDepth = (reports?: UZIReportItem[], depth?: UZIDepth) => {
    return (reports || []).find(report => !depth || normalizeReportDepth(report.depth) === depth);
};

export const isUZIBackgroundProcessingError = (message?: string | null) => {
    const normalized = (message || '').toLowerCase();
    return normalized.includes('bad gateway')
        || normalized.includes('502')
        || normalized.includes('409')
        || normalized.includes('conflict')
        || normalized.includes('已有研报生成请求正在处理')
        || normalized.includes('research report generation request')
        || normalized.includes('failed to fetch')
        || normalized.includes('networkerror')
        || normalized.includes('stream closed before completion')
        || normalized.includes('unexpected uzi analyze response content-type')
        || normalized.includes('timeout')
        || normalized.includes('timed out');
};

export const getUserFacingError = (message?: string | null, language: Language = 'zh') => {
    const isZh = language === 'zh';
    const normalized = (message || '').trim();
    if (!normalized) {
        return isZh ? '当前请求未完成，请稍后再试。' : 'The request is still pending. Please try again later.';
    }

    if (normalized.includes('Authorization header required')
        || normalized.includes('Unauthorized')
        || normalized.includes('401')
    ) {
        return isZh ? '登录状态已失效，请重新登录后继续。' : 'Your session has expired. Please sign in again.';
    }

    if (normalized.includes('请先在设置中配置 AI 模型')
        || normalized.includes('AI model config')
        || normalized.includes('api_key is required')
    ) {
        return isZh ? '请先在设置中配置 AI 模型后再生成研报。' : 'Configure the AI model in Settings before generating reports.';
    }

    if (normalized.includes('not configured')
        || normalized.includes('disabled')
        || normalized.includes('Bad Gateway')
        || normalized.includes('call uzi')
        || normalized.includes('stream closed before completion')
        || normalized.includes('unexpected uzi analyze response content-type')
    ) {
        return isZh ? '研报服务暂时不可用，请稍后刷新后再试。' : 'The report service is temporarily unavailable. Refresh and try again later.';
    }

    if (normalized.includes('Browser blocked opening a new tab')) {
        return isZh ? '浏览器拦截了新标签页，请允许站点打开新页面后重试。' : 'The browser blocked the new tab. Allow pop-ups for this site and try again.';
    }

    if (normalized.includes('report open token has expired')) {
        return isZh ? '打开链接已失效，请重新点击打开报告。' : 'The open link has expired. Open the report again.';
    }

    if (normalized.includes('report open token is invalid')) {
        return isZh ? '打开链接无效，请重新点击打开报告。' : 'The open link is invalid. Open the report again.';
    }

    return isZh ? '当前请求未完成，请稍后再试。' : 'The request is still pending. Please try again later.';
};

export const getReportDisplayTitle = (ticker: string, dateTag?: string, language: Language = 'zh') => {
    const suffix = language === 'zh' ? '研报' : 'Report';
    if (dateTag) {
        return `${ticker} ${suffix} · ${dateTag}`;
    }
    return `${ticker} ${suffix}`;
};
