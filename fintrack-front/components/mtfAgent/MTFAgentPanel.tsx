import React, { useEffect, useRef, useState } from 'react';
import { useLanguage } from '../../contexts/LanguageContext';
import { mtfAgentAPI, MTFAgentMemory, MTFAgentMessage } from '../../services/apiService';
import MarkdownMessage, { isLikelyMarkdown } from './MarkdownMessage';

interface MTFAgentPanelProps {
    onAuthError?: () => void;
    onOpenSettings?: () => void;
    onClose?: () => void;
    className?: string;
}

const AGENT_GOLD_GRADIENT = 'linear-gradient(135deg, #FFF1B8 0%, #FCD34D 34%, #F59E0B 66%, #F97316 100%)';
const STREAM_RENDER_CHARS_PER_TICK = 10;
const STREAM_RENDER_INTERVAL_MS = 18;

type StaticQuickPhrase = {
    label: string;
    prompt: string;
};

type TargetQuickPhrase = {
    label: string;
    targetPlaceholder: string;
    buildPrompt: (target: string) => string;
};

type QuickPhrase = StaticQuickPhrase | TargetQuickPhrase;

const hasTargetPrompt = (item: QuickPhrase): item is TargetQuickPhrase => 'buildPrompt' in item;

const singleTargetPlaceholder = '如 688017 / 000858 / 茅台';

const aStockQuickPhrases: QuickPhrase[] = [
    { label: '个股估值', targetPlaceholder: singleTargetPlaceholder, buildPrompt: target => `帮我估一下 ${target}，给我 PE / PEG / 消化时间` },
    { label: '题材归因', prompt: '今天哪些股票走强，主要是什么题材' },
    { label: '全市场龙虎榜', prompt: '今天龙虎榜哪些票净买入最多' },
];

const isAuthError = (message: string) => (
    message.includes('Authorization header required') ||
    message.trim() === 'Unauthorized' ||
    message.trim() === 'HTTP error! status: 401'
);

const MTFAgentPanel: React.FC<MTFAgentPanelProps> = ({
    onAuthError,
    onOpenSettings,
    onClose,
    className = '',
}) => {
    const { t } = useLanguage();
    const [loadingSession, setLoadingSession] = useState(false);
    const [sending, setSending] = useState(false);
    const [resetting, setResetting] = useState(false);
    const [input, setInput] = useState('');
    const [error, setError] = useState('');
    const [modelID, setModelID] = useState('');
    const [runtimeAvailable, setRuntimeAvailable] = useState(false);
    const [hasAIModelConfig, setHasAIModelConfig] = useState(true);
    const [showMemory, setShowMemory] = useState(false);
    const [loadingMemory, setLoadingMemory] = useState(false);
    const [confirmReset, setConfirmReset] = useState(false);
    const [pendingQuickPhrase, setPendingQuickPhrase] = useState<TargetQuickPhrase | null>(null);
    const [quickPhraseTarget, setQuickPhraseTarget] = useState('');
    const [quickPhraseError, setQuickPhraseError] = useState('');
    const [messages, setMessages] = useState<MTFAgentMessage[]>([]);
    const [memories, setMemories] = useState<MTFAgentMemory[]>([]);
    const messageEndRef = useRef<HTMLDivElement | null>(null);
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    const quickPhraseInputRef = useRef<HTMLInputElement | null>(null);

    useEffect(() => {
        let cancelled = false;
        const loadSession = async () => {
            setLoadingSession(true);
            setError('');
            try {
                const session = await mtfAgentAPI.getSession();
                if (cancelled) return;
                setModelID(session.model_id || '');
                setRuntimeAvailable(session.runtime_available);
                setHasAIModelConfig(session.has_ai_model_config);
                if (!session.has_ai_model_config) {
                    setError(t('mtfAgent.configRequired'));
                } else if (!session.runtime_available) {
                    setError(t('mtfAgent.runtimeUnavailable'));
                }
                const history = await mtfAgentAPI.getMessages();
                if (cancelled) return;
                setMessages(history.messages || []);
            } catch (err: any) {
                const message = err?.message || t('mtfAgent.unavailable');
                if (isAuthError(message)) onAuthError?.();
                if (!cancelled) setError(message);
            } finally {
                if (!cancelled) setLoadingSession(false);
            }
        };
        loadSession();
        return () => {
            cancelled = true;
        };
    }, [onAuthError, t]);

    useEffect(() => {
        messageEndRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' });
    }, [messages, sending]);

    const handleSend = async () => {
        const text = input.trim();
        if (!text || sending) return;
        const assistantDraftID = -Date.now();
        let assistantContent = '';
        let pendingContent = '';
        let renderTimer: number | null = null;
        let streamEnded = false;
        let drainResolve: (() => void) | null = null;
        const applyAssistantContent = (content: string) => {
            setMessages(prev => prev.map(message => (
                message.id === assistantDraftID
                    ? { ...message, content }
                    : message
            )));
        };
        const resolveDrainIfReady = () => {
            if (!streamEnded || pendingContent || renderTimer) return;
            const resolve = drainResolve;
            drainResolve = null;
            resolve?.();
        };
        const pumpPendingContent = () => {
            if (!pendingContent) {
                renderTimer = null;
                resolveDrainIfReady();
                return;
            }
            const nextChunk = pendingContent.slice(0, STREAM_RENDER_CHARS_PER_TICK);
            pendingContent = pendingContent.slice(nextChunk.length);
            assistantContent += nextChunk;
            applyAssistantContent(assistantContent);
            renderTimer = window.setTimeout(pumpPendingContent, STREAM_RENDER_INTERVAL_MS);
        };
        const enqueueAssistantDelta = (chunk: string) => {
            pendingContent += chunk;
            if (!renderTimer) {
                pumpPendingContent();
            }
        };
        const waitForRenderDrain = () => new Promise<void>(resolve => {
            streamEnded = true;
            if (!pendingContent && !renderTimer) {
                resolve();
                return;
            }
            drainResolve = resolve;
        });
        setInput('');
        setError('');
        setPendingQuickPhrase(null);
        setQuickPhraseTarget('');
        setQuickPhraseError('');
        setMessages(prev => [
            ...prev,
            { role: 'user', content: text },
            { id: assistantDraftID, role: 'assistant', content: '' },
        ]);
        setSending(true);
        try {
            const response = await mtfAgentAPI.sendMessage(text, {
                onDelta: enqueueAssistantDelta,
            });
            await waitForRenderDrain();
            const emptyAssistantMessage = t('mtfAgent.emptyResponse');
            const finalMessage = response.message || { role: 'assistant' as const, content: assistantContent || emptyAssistantMessage };
            setMessages(prev => prev.map(message => (
                message.id === assistantDraftID
                    ? { ...finalMessage, content: finalMessage.content || assistantContent || emptyAssistantMessage }
                    : message
            )));
            if (response.model) setModelID(response.model);
        } catch (err: any) {
            if (renderTimer) {
                window.clearTimeout(renderTimer);
                renderTimer = null;
            }
            const message = err?.message || t('mtfAgent.unavailable');
            if (isAuthError(message)) onAuthError?.();
            if (!assistantContent && !pendingContent) {
                setMessages(prev => prev.filter(item => item.id !== assistantDraftID));
            }
            setError(message);
        } finally {
            setSending(false);
        }
    };

    const handleReset = async () => {
        setError('');
        setResetting(true);
        try {
            const response = await mtfAgentAPI.reset();
            setMessages([]);
            setConfirmReset(false);
            if (response.thread_id) setError(t('mtfAgent.resetSuccess'));
        } catch (err: any) {
            const message = err?.message || t('mtfAgent.resetFailed');
            if (isAuthError(message)) onAuthError?.();
            setError(message);
        } finally {
            setResetting(false);
        }
    };

    const handleToggleMemory = async () => {
        const nextVisible = !showMemory;
        setShowMemory(nextVisible);
        if (!nextVisible || memories.length > 0) return;
        setLoadingMemory(true);
        setError('');
        try {
            const response = await mtfAgentAPI.getMemory();
            setMemories(response.items || []);
        } catch (err: any) {
            const message = err?.message || t('mtfAgent.memoryLoadFailed');
            if (isAuthError(message)) onAuthError?.();
            setError(message);
        } finally {
            setLoadingMemory(false);
        }
    };

    const handleClearMemory = async () => {
        setLoadingMemory(true);
        setError('');
        try {
            await mtfAgentAPI.clearMemory();
            setMemories([]);
        } catch (err: any) {
            const message = err?.message || t('mtfAgent.memoryClearFailed');
            if (isAuthError(message)) onAuthError?.();
            setError(message);
        } finally {
            setLoadingMemory(false);
        }
    };

    const disabledReason = !hasAIModelConfig ? t('mtfAgent.configRequired') : !runtimeAvailable ? t('mtfAgent.waitRuntime') : '';
    const canSend = Boolean(input.trim()) && !sending && !disabledReason;
    const handleQuickPhrase = (item: QuickPhrase) => {
        if (hasTargetPrompt(item)) {
            setPendingQuickPhrase(item);
            setQuickPhraseTarget('');
            setQuickPhraseError('');
            window.setTimeout(() => quickPhraseInputRef.current?.focus(), 0);
            return;
        }
        setPendingQuickPhrase(null);
        setQuickPhraseTarget('');
        setQuickPhraseError('');
        setInput(item.prompt);
        window.setTimeout(() => inputRef.current?.focus(), 0);
    };
    const handleConfirmQuickPhraseTarget = () => {
        if (!pendingQuickPhrase) return;
        const target = quickPhraseTarget.trim();
        if (!target) {
            setQuickPhraseError('请输入股票代码或者股票名称');
            window.setTimeout(() => quickPhraseInputRef.current?.focus(), 0);
            return;
        }
        setInput(pendingQuickPhrase.buildPrompt(target));
        setPendingQuickPhrase(null);
        setQuickPhraseTarget('');
        setQuickPhraseError('');
        window.setTimeout(() => inputRef.current?.focus(), 0);
    };
    const handleCancelQuickPhraseTarget = () => {
        setPendingQuickPhrase(null);
        setQuickPhraseTarget('');
        setQuickPhraseError('');
        window.setTimeout(() => inputRef.current?.focus(), 0);
    };

    return (
        <section className={`flex min-h-0 flex-col bg-[#101417] ${className}`}>
            <header className="flex items-center justify-between gap-3 border-b border-white/10 px-4 py-3 sm:px-5">
                <div className="min-w-0">
                    <h2 id="mtf-agent-title" className="truncate text-lg font-black text-white">{t('mtfAgent.title')}</h2>
                    <p className="mt-1 truncate text-xs text-white/45">{modelID || 'DeepSeek-TUI runtime'}</p>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                    <button type="button" title={t('mtfAgent.memory')} onClick={handleToggleMemory} className="rounded-lg p-2 text-white/70 transition-colors hover:bg-white/10 hover:text-white">
                        <span className="material-symbols-outlined text-[20px]">memory</span>
                    </button>
                    <button type="button" title={t('mtfAgent.reset')} onClick={() => setConfirmReset(true)} className="rounded-lg p-2 text-white/70 transition-colors hover:bg-white/10 hover:text-white">
                        <span className="material-symbols-outlined text-[20px]">restart_alt</span>
                    </button>
                    {onClose && (
                        <button type="button" title={t('common.close')} onClick={onClose} className="rounded-lg p-2 text-white/70 transition-colors hover:bg-white/10 hover:text-white">
                            <span className="material-symbols-outlined text-[20px]">close</span>
                        </button>
                    )}
                </div>
            </header>

            {showMemory && (
                <section className="border-b border-white/10 bg-white/[0.03] px-4 py-3 sm:px-5">
                    <div className="mb-2 flex items-center justify-between gap-3">
                        <span className="text-sm font-bold text-white">{t('mtfAgent.memoryTitle')}</span>
                        <button type="button" onClick={handleClearMemory} disabled={loadingMemory || memories.length === 0} className="rounded-lg px-2 py-1 text-xs text-red-100 transition-colors hover:bg-red-500/15 disabled:cursor-not-allowed disabled:text-white/30">
                            {t('common.clear')}
                        </button>
                    </div>
                    {loadingMemory ? <p className="text-sm text-white/45">{t('mtfAgent.loadingMemory')}</p> : memories.length === 0 ? (
                        <p className="text-sm text-white/45">{t('mtfAgent.emptyMemory')}</p>
                    ) : (
                        <div className="max-h-32 space-y-2 overflow-y-auto pr-1 text-sm text-white/70">
                            {memories.map(memory => <p key={memory.id} className="leading-5">{memory.content}</p>)}
                        </div>
                    )}
                </section>
            )}

            <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4 sm:px-5">
                {loadingSession && <p className="text-sm text-white/45">{t('mtfAgent.connecting')}</p>}
                {!loadingSession && messages.length === 0 && (
                    <div className="rounded-lg border border-white/10 bg-white/[0.04] p-4 text-sm leading-6 text-white/65">
                        {t('mtfAgent.emptyHint')}
                    </div>
                )}
                <div className="mt-3 flex flex-col gap-3">
                    {messages.map((message, index) => {
                        const isAssistant = message.role === 'assistant';
                        const isStreamingDraft = isAssistant && sending && typeof message.id === 'number' && message.id < 0;
                        const renderMarkdown = isAssistant && !isStreamingDraft && isLikelyMarkdown(message.content || '');
                        return (
                            <div
                                key={message.id ? `${message.role}-${message.id}` : `${message.role}-${index}`}
                                className={message.role === 'user'
                                    ? 'max-w-[86%] self-end rounded-lg px-4 py-3 text-sm font-semibold leading-6 text-[#18130b] shadow-[0_10px_28px_rgba(245,158,11,0.18)]'
                                    : `max-w-[88%] self-start rounded-lg border border-white/10 bg-white/[0.06] px-4 py-3 text-sm leading-6 text-white/82 ${renderMarkdown ? '' : 'whitespace-pre-wrap break-words'}`}
                                style={message.role === 'user' ? { background: AGENT_GOLD_GRADIENT } : undefined}
                            >
                                {renderMarkdown
                                    ? <MarkdownMessage content={message.content} />
                                    : (message.content || (isAssistant && sending ? t('mtfAgent.thinking') : ''))}
                            </div>
                        );
                    })}
                    <div ref={messageEndRef} />
                </div>
            </div>

            {confirmReset && (
                <div className="border-t border-amber-300/20 bg-amber-300/10 px-4 py-3 text-sm text-amber-50 sm:px-5">
                    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                        <span>{t('mtfAgent.resetConfirmText')}</span>
                        <div className="flex gap-2">
                            <button type="button" onClick={() => setConfirmReset(false)} className="rounded-lg px-3 py-2 text-white/70 hover:bg-white/10">{t('common.cancel')}</button>
                            <button type="button" onClick={handleReset} disabled={resetting} className="rounded-lg bg-amber-200 px-3 py-2 font-semibold text-[#18130b] disabled:opacity-60">
                                {resetting ? t('mtfAgent.resetting') : t('mtfAgent.confirmReset')}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {error && (
                <div role="alert" aria-live="polite" className="border-t border-red-400/20 bg-red-500/10 px-4 py-3 text-sm text-red-100 sm:px-5">
                    <div className="flex items-center justify-between gap-3">
                        <span>{error}</span>
                        {!hasAIModelConfig && onOpenSettings && (
                            <button type="button" onClick={onOpenSettings} className="shrink-0 rounded-lg bg-white/10 px-3 py-2 text-xs font-semibold text-white hover:bg-white/15">
                                {t('mtfAgent.goSettings')}
                            </button>
                        )}
                    </div>
                </div>
            )}

            <footer className="border-t border-white/10 p-4">
                {disabledReason && <p className="mb-2 text-xs text-white/45">{disabledReason}</p>}
                <div className="mb-3 flex gap-2 overflow-x-auto overscroll-x-contain pb-1 no-scrollbar" aria-label="A 股数据快捷短语">
                    {aStockQuickPhrases.map(item => (
                        <button
                            key={item.label}
                            type="button"
                            onClick={() => handleQuickPhrase(item)}
                            title={hasTargetPrompt(item) ? '请输入股票代码或者股票名称' : item.prompt}
                            aria-pressed={pendingQuickPhrase?.label === item.label}
                            className="group flex h-9 max-w-[220px] shrink-0 items-center gap-2 rounded-full border border-amber-200/18 bg-amber-200/[0.07] px-3 text-xs font-semibold text-amber-100 transition-colors hover:border-amber-200/38 hover:bg-amber-200/[0.12] focus:outline-none focus:ring-2 focus:ring-amber-200/40"
                        >
                            <span className="material-symbols-outlined text-[16px] text-amber-200/90">auto_awesome</span>
                            <span className="truncate">{item.label}</span>
                        </button>
                    ))}
                </div>
                {pendingQuickPhrase && (
                    <div className="mb-3 rounded-lg border border-amber-200/20 bg-amber-200/[0.06] p-2">
                        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                            <label className="shrink-0 text-xs font-semibold text-amber-100" htmlFor="mtf-agent-quick-phrase-target">
                                请输入股票代码或者股票名称
                            </label>
                            <input
                                id="mtf-agent-quick-phrase-target"
                                ref={quickPhraseInputRef}
                                value={quickPhraseTarget}
                                onChange={event => {
                                    setQuickPhraseTarget(event.target.value);
                                    if (quickPhraseError) setQuickPhraseError('');
                                }}
                                onKeyDown={event => {
                                    if (event.key === 'Enter') {
                                        event.preventDefault();
                                        handleConfirmQuickPhraseTarget();
                                    }
                                    if (event.key === 'Escape') {
                                        event.preventDefault();
                                        handleCancelQuickPhraseTarget();
                                    }
                                }}
                                placeholder={pendingQuickPhrase.targetPlaceholder}
                                className="min-h-9 flex-1 rounded-md border border-white/10 bg-black/20 px-3 text-sm text-white outline-none placeholder:text-white/35 focus:border-amber-200/65"
                            />
                            <div className="flex shrink-0 items-center gap-2">
                                <button
                                    type="button"
                                    onClick={handleCancelQuickPhraseTarget}
                                    title={t('common.cancel')}
                                    className="flex h-9 w-9 items-center justify-center rounded-md text-white/65 transition-colors hover:bg-white/10 hover:text-white focus:outline-none focus:ring-2 focus:ring-white/20"
                                >
                                    <span className="material-symbols-outlined text-[18px]">close</span>
                                </button>
                                <button
                                    type="button"
                                    onClick={handleConfirmQuickPhraseTarget}
                                    title="确认"
                                    className="flex h-9 w-9 items-center justify-center rounded-md text-[#18130b] transition-opacity hover:opacity-95 focus:outline-none focus:ring-2 focus:ring-amber-200/40"
                                    style={{ background: AGENT_GOLD_GRADIENT }}
                                >
                                    <span className="material-symbols-outlined text-[18px]">check</span>
                                </button>
                            </div>
                        </div>
                        {quickPhraseError && <p className="mt-2 text-xs font-semibold text-amber-100">{quickPhraseError}</p>}
                    </div>
                )}
                <div className="flex items-end gap-2">
                    <textarea
                        ref={inputRef}
                        value={input}
                        onChange={event => setInput(event.target.value)}
                        onKeyDown={event => {
                            if (event.key === 'Enter' && !event.shiftKey) {
                                event.preventDefault();
                                handleSend();
                            }
                        }}
                        placeholder={t('mtfAgent.placeholder')}
                        rows={1}
                        className="min-h-[48px] flex-1 resize-none rounded-lg border border-white/10 bg-black/20 px-4 py-3 text-sm text-white outline-none placeholder:text-white/35 focus:border-amber-200/65"
                    />
                    <button
                        type="button"
                        onClick={handleSend}
                        disabled={!canSend}
                        className="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg text-[#18130b] transition-opacity hover:opacity-95 disabled:cursor-not-allowed disabled:opacity-45"
                        style={{ background: AGENT_GOLD_GRADIENT }}
                    >
                        <span className="material-symbols-outlined text-[20px]">{sending ? 'hourglass_top' : 'send'}</span>
                    </button>
                </div>
            </footer>
        </section>
    );
};

export default MTFAgentPanel;
