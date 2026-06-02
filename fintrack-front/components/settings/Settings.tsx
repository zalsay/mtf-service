import React, { useEffect, useState } from 'react';
import LanguageSwitcher from '../layout/LanguageSwitcher';
import { useLanguage } from '../../contexts/LanguageContext';
import { settingsAPI } from '../../services/apiService';
import InviteRedeemCard from './InviteRedeemCard';

const RECOMMENDED_AI_BASE_URL = 'https://api.deepseek.com';
const RECOMMENDED_AI_MODEL_ID = 'deepseek-v4-pro';

const normalizeAIModelID = (value?: string | null) => {
    const normalized = (value || '').trim();
    if (!normalized || normalized.includes('[') || normalized.includes(']')) {
        return RECOMMENDED_AI_MODEL_ID;
    }
    return normalized;
};

interface SettingsProps {
    onLogout: () => void;
    isDemoMode?: boolean;
}

type SettingsTab = 'model' | 'account';

const Settings: React.FC<SettingsProps> = ({ onLogout, isDemoMode = false }) => {
    const { t, language } = useLanguage();
    const [activeTab, setActiveTab] = useState<SettingsTab>('model');
    const [baseUrl, setBaseUrl] = useState(RECOMMENDED_AI_BASE_URL);
    const [apiKey, setApiKey] = useState('');
    const [apiKeyMasked, setApiKeyMasked] = useState('');
    const [modelId, setModelId] = useState(RECOMMENDED_AI_MODEL_ID);
    const [hasApiKey, setHasApiKey] = useState(false);
    const [isRecommended, setIsRecommended] = useState(true);
    const [loadingAIConfig, setLoadingAIConfig] = useState(true);
    const [savingAIConfig, setSavingAIConfig] = useState(false);
    const [aiConfigMessage, setAIConfigMessage] = useState('');
    const [aiConfigError, setAIConfigError] = useState('');

    const applyRecommendedConfig = () => {
        setBaseUrl(RECOMMENDED_AI_BASE_URL);
        setModelId(RECOMMENDED_AI_MODEL_ID);
        setIsRecommended(true);
    };

    useEffect(() => {
        let cancelled = false;

        const loadAIConfig = async () => {
            setLoadingAIConfig(true);
            setAIConfigError('');
            try {
                const config = await settingsAPI.getAIModelConfig();
                if (cancelled) {
                    return;
                }
                const nextBaseUrl = config.base_url || RECOMMENDED_AI_BASE_URL;
                const nextModelId = normalizeAIModelID(config.model_id);
                const isRecommendedConfig = nextBaseUrl.replace(/\/+$/, '') === RECOMMENDED_AI_BASE_URL
                    && nextModelId === RECOMMENDED_AI_MODEL_ID;
                setBaseUrl(nextBaseUrl);
                setModelId(nextModelId);
                setApiKey('');
                setApiKeyMasked(config.api_key_masked || '');
                setHasApiKey(Boolean(config.has_api_key));
                setIsRecommended(Boolean(config.is_recommended) || isRecommendedConfig);
            } catch (error: any) {
                if (!cancelled) {
                    setAIConfigError(error?.message || t('settings.aiLoadFailed', '加载 AI 模型配置失败'));
                }
            } finally {
                if (!cancelled) {
                    setLoadingAIConfig(false);
                }
            }
        };

        loadAIConfig();

        return () => {
            cancelled = true;
        };
    }, [t]);

    const handleSaveAIConfig = async () => {
        setSavingAIConfig(true);
        setAIConfigMessage('');
        setAIConfigError('');
        try {
            const nextModelId = normalizeAIModelID(modelId);
            setModelId(nextModelId);
            const config = await settingsAPI.updateAIModelConfig({
                base_url: baseUrl,
                api_key: apiKey,
                model_id: nextModelId,
            });
            setApiKey('');
            setApiKeyMasked(config.api_key_masked || '');
            setHasApiKey(Boolean(config.has_api_key));
            setIsRecommended(Boolean(config.is_recommended));
            setAIConfigMessage(t('settings.aiSaved', 'AI 模型配置已保存'));
        } catch (error: any) {
            setAIConfigError(error?.message || t('settings.aiSaveFailed', '保存 AI 模型配置失败'));
        } finally {
            setSavingAIConfig(false);
        }
    };

    const tabs: Array<{ id: SettingsTab; label: string; icon: string }> = [
        { id: 'model', label: t('settings.modelConfigTab', '模型配置'), icon: 'auto_awesome' },
        { id: 'account', label: t('settings.accountSettingsTab', '账号设置'), icon: 'manage_accounts' },
    ];

    return (
        <div className="flex flex-col gap-6">
            <header className="flex flex-col gap-1">
                <h1 className="text-white text-4xl font-black leading-tight tracking-[-0.033em]">
                    {t('settings.title', '设置')}
                </h1>
                <p className="text-white/60 text-base leading-normal">
                    {t('settings.subtitle', '管理语言偏好与登录状态。')}
                </p>
            </header>

            <div className="inline-flex w-full rounded-2xl border border-white/10 bg-black/20 p-1 sm:w-fit">
                {tabs.map((tab) => (
                    <button
                        key={tab.id}
                        type="button"
                        onClick={() => setActiveTab(tab.id)}
                        className={`flex h-11 flex-1 items-center justify-center gap-2 rounded-xl px-4 text-sm font-bold transition-colors sm:flex-none ${
                            activeTab === tab.id
                                ? 'bg-primary text-background-dark'
                                : 'text-white/55 hover:bg-white/5 hover:text-white'
                        }`}
                    >
                        <span className="material-symbols-outlined text-[18px]">{tab.icon}</span>
                        <span>{tab.label}</span>
                    </button>
                ))}
            </div>

            {activeTab === 'model' && (
            <section className="premium-gold-card overflow-hidden rounded-3xl border p-5">
                <div className="mb-5 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                    <div className="flex items-start gap-3">
                        <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl bg-amber-300/15 text-amber-200 ring-1 ring-amber-200/20">
                            <span className="material-symbols-outlined">auto_awesome</span>
                        </div>
                        <div>
                            <div className="mb-1 flex flex-wrap items-center gap-2">
                                <h2 className="text-white text-lg font-bold leading-normal">
                                    {t('settings.aiModelTitle', 'AI 模型配置')}
                                </h2>
                                {isRecommended && (
                                    <span className="rounded-full border border-amber-200/30 bg-amber-300/15 px-2.5 py-1 text-xs font-bold text-amber-100">
                                        {t('settings.aiRecommended', '推荐')}
                                    </span>
                                )}
                            </div>
                            <p className="text-white/50 text-sm leading-normal">
                                {t('settings.aiModelDesc', '配置用于智能分析的兼容 OpenAI 接口模型。')}
                            </p>
                        </div>
                    </div>
                    <button
                        type="button"
                        onClick={applyRecommendedConfig}
                        className="inline-flex h-10 items-center justify-center gap-2 rounded-xl border border-amber-200/20 bg-amber-300/10 px-4 text-sm font-bold text-amber-100 transition-colors hover:bg-amber-300/15"
                    >
                        <span className="material-symbols-outlined text-[16px]">stars</span>
                        <span>DeepSeek v4 pro</span>
                    </button>
                </div>

                <div className="grid gap-4 lg:grid-cols-[1.2fr_0.8fr]">
                    <label className="flex flex-col gap-2">
                        <span className="text-xs font-bold uppercase tracking-[0.18em] text-white/35">Base URL</span>
                        <input
                            value={baseUrl}
                            onChange={(event) => {
                                setBaseUrl(event.target.value);
                                setIsRecommended(false);
                            }}
                            disabled={loadingAIConfig}
                            placeholder={RECOMMENDED_AI_BASE_URL}
                            className="h-12 rounded-2xl border border-white/10 bg-black/20 px-4 text-sm font-medium text-white outline-none transition-colors placeholder:text-white/25 focus:border-amber-200/45"
                        />
                    </label>
                    <label className="flex flex-col gap-2">
                        <span className="text-xs font-bold uppercase tracking-[0.18em] text-white/35">Model ID</span>
                        <input
                            value={modelId}
                            onChange={(event) => {
                                setModelId(event.target.value);
                                setIsRecommended(false);
                            }}
                            disabled={loadingAIConfig}
                            placeholder="deepseek-v4-pro"
                            className="h-12 rounded-2xl border border-white/10 bg-black/20 px-4 text-sm font-medium text-white outline-none transition-colors placeholder:text-white/25 focus:border-amber-200/45"
                        />
                    </label>
                    <label className="flex flex-col gap-2 lg:col-span-2">
                        <div className="flex items-center justify-between gap-3">
                            <span className="text-xs font-bold uppercase tracking-[0.18em] text-white/35">API Key</span>
                            {hasApiKey && (
                                <span className="text-xs font-semibold text-emerald-200/80">
                                    {language === 'zh' ? `已保存 ${apiKeyMasked}` : `Saved ${apiKeyMasked}`}
                                </span>
                            )}
                        </div>
                        <input
                            value={apiKey}
                            onChange={(event) => setApiKey(event.target.value)}
                            disabled={loadingAIConfig}
                            type="password"
                            placeholder={hasApiKey ? t('settings.aiKeyKeepPlaceholder', '留空则继续使用已保存密钥') : 'sk-...'}
                            className="h-12 rounded-2xl border border-white/10 bg-black/20 px-4 text-sm font-medium text-white outline-none transition-colors placeholder:text-white/25 focus:border-amber-200/45"
                        />
                    </label>
                </div>

                <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                    <div className="min-h-5 text-sm">
                        {aiConfigError && <span className="text-red-200">{aiConfigError}</span>}
                        {!aiConfigError && aiConfigMessage && <span className="text-emerald-200">{aiConfigMessage}</span>}
                    </div>
                    <button
                        type="button"
                        onClick={handleSaveAIConfig}
                        disabled={loadingAIConfig || savingAIConfig}
                        className="inline-flex h-11 items-center justify-center gap-2 rounded-2xl bg-amber-300 px-5 text-sm font-black text-black transition-colors hover:bg-amber-200 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                        <span className="material-symbols-outlined text-[18px]">save</span>
                        <span>
                            {savingAIConfig
                                ? t('settings.saving', '保存中...')
                                : t('settings.saveAIConfig', '保存配置')}
                        </span>
                    </button>
                </div>
            </section>
            )}

            {activeTab === 'account' && (
            <section className="grid gap-4 md:grid-cols-2">
                <InviteRedeemCard isDemoMode={isDemoMode} />

                <div className="rounded-2xl border border-white/10 bg-card-dark p-5">
                    <div className="mb-4 flex items-center gap-3">
                        <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl bg-white/6 text-white/80">
                            <span className="material-symbols-outlined">language</span>
                        </div>
                        <div>
                            <h2 className="text-white text-lg font-bold leading-normal">
                                {t('settings.languageTitle', '语言')}
                            </h2>
                            <p className="text-white/50 text-sm leading-normal">
                                {t('settings.languageDesc', '切换界面显示语言。')}
                            </p>
                        </div>
                    </div>
                    <LanguageSwitcher variant="embedded" />
                </div>

                <div className="rounded-2xl border border-white/10 bg-card-dark p-5">
                    <div className="mb-4 flex items-center gap-3">
                        <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl bg-red-500/10 text-red-100">
                            <span className="material-symbols-outlined">logout</span>
                        </div>
                        <div>
                            <h2 className="text-white text-lg font-bold leading-normal">
                                {t('settings.accountTitle', '账户')}
                            </h2>
                            <p className="text-white/50 text-sm leading-normal">
                                {isDemoMode
                                    ? t('settings.demoDesc', '当前为演示模式，退出后将返回首页。')
                                    : t('settings.accountDesc', '安全退出当前账号。')}
                            </p>
                        </div>
                    </div>
                    <button
                        type="button"
                        onClick={onLogout}
                        className="flex h-11 w-full items-center justify-center gap-2 rounded-xl border border-red-400/20 bg-red-500/10 text-sm font-bold text-red-200 transition-colors hover:bg-red-500/15"
                    >
                        <span className="material-symbols-outlined text-[18px]">logout</span>
                        <span>{t('common.logout')}</span>
                    </button>
                </div>
            </section>
            )}
        </div>
    );
};

export default Settings;
