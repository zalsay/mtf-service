import React, {
    forwardRef,
    useEffect,
    useImperativeHandle,
    useMemo,
    useRef,
    useState,
} from 'react';
import { useLanguage } from '../../contexts/LanguageContext';

type CaptchaStatus = 'disabled' | 'loading' | 'ready' | 'error';

interface AliyunCaptchaInstance {
    refresh?: () => void;
}

interface AliyunCaptchaVerifyObject {
    captchaVerifyParam?: string;
    CaptchaVerifyParam?: string;
}

type AliyunCaptchaVerifyResult = string | AliyunCaptchaVerifyObject;

interface AliyunCaptchaCallbackResult {
    captchaResult: boolean;
    bizResult?: boolean;
}

interface AliyunCaptchaOptions {
    SceneId: string;
    prefix: string;
    mode: string;
    element: string;
    button: string;
    region?: string;
    server?: string[];
    slideStyle?: {
        width?: number;
        height?: number;
    };
    getInstance?: (instance: AliyunCaptchaInstance) => void;
    captchaVerifyCallback?: (
        captchaVerifyParam: AliyunCaptchaVerifyResult,
        callback?: (result: AliyunCaptchaCallbackResult) => void
    ) => AliyunCaptchaCallbackResult | Promise<AliyunCaptchaCallbackResult> | void;
    onBizResultCallback?: () => void;
    success?: (result: AliyunCaptchaVerifyResult) => void;
    fail?: (result: unknown) => void;
    onError?: (errorInfo: { code?: string; msg?: string }) => void;
    onClose?: () => void;
}

interface AliyunCaptchaConfig {
    enabled: boolean;
    prefix: string;
    sceneId: string;
    region: string;
    server?: string[];
    scriptUrl: string;
}

export interface AliyunCaptchaHandle {
    verify: () => Promise<string>;
    reset: () => void;
}

interface AliyunCaptchaProps {
    disabled?: boolean;
}

declare global {
    interface Window {
        AliyunCaptchaConfig?: { region: string; prefix: string };
        initAliyunCaptcha?: (options: AliyunCaptchaOptions) => void;
    }
}

const CAPTCHA_SCRIPT_ID = 'aliyun-captcha-script';
let captchaScriptPromise: Promise<void> | null = null;

const getEnvValue = (key: string, fallback = ''): string => {
    const value = (import.meta as any).env?.[key];
    return typeof value === 'string' ? value.trim() : fallback;
};

export const getAliyunCaptchaConfig = (): AliyunCaptchaConfig => {
    const prefix = getEnvValue('VITE_ALIYUN_CAPTCHA_PREFIX');
    const sceneId = getEnvValue('VITE_ALIYUN_CAPTCHA_SCENE_ID');
    const enabledFlag = getEnvValue('VITE_ALIYUN_CAPTCHA_ENABLED', 'true').toLowerCase();
    const serverValue = getEnvValue('VITE_ALIYUN_CAPTCHA_SERVER');

    return {
        enabled: enabledFlag !== 'false' && Boolean(prefix && sceneId),
        prefix,
        sceneId,
        region: getEnvValue('VITE_ALIYUN_CAPTCHA_REGION', 'cn'),
        server: serverValue
            ? serverValue.split(',').map((server) => server.trim()).filter(Boolean)
            : undefined,
        scriptUrl: getEnvValue(
            'VITE_ALIYUN_CAPTCHA_SCRIPT_URL',
            'https://o.alicdn.com/captcha-frontend/aliyunCaptcha/AliyunCaptcha.js'
        ),
    };
};

const loadCaptchaScript = (config: AliyunCaptchaConfig): Promise<void> => {
    if (typeof window === 'undefined') {
        return Promise.reject(new Error('Captcha is unavailable outside the browser'));
    }

    window.AliyunCaptchaConfig = {
        region: config.region,
        prefix: config.prefix,
    };

    if (window.initAliyunCaptcha) {
        return Promise.resolve();
    }

    if (!captchaScriptPromise) {
        captchaScriptPromise = new Promise((resolve, reject) => {
            const existingScript = document.getElementById(CAPTCHA_SCRIPT_ID) as HTMLScriptElement | null;
            if (existingScript) {
                if (window.initAliyunCaptcha) {
                    resolve();
                    return;
                }
                existingScript.addEventListener('load', () => resolve(), { once: true });
                existingScript.addEventListener('error', () => reject(new Error('Failed to load captcha script')), { once: true });
                return;
            }

            const script = document.createElement('script');
            script.id = CAPTCHA_SCRIPT_ID;
            script.src = config.scriptUrl;
            script.async = true;
            script.onload = () => {
                script.dataset.loaded = 'true';
                resolve();
            };
            script.onerror = () => reject(new Error('Failed to load captcha script'));
            document.body.appendChild(script);
        });
    }

    return captchaScriptPromise;
};

const getCaptchaVerifyParam = (result: AliyunCaptchaVerifyResult): string => {
    if (typeof result === 'string') {
        return result;
    }
    return result?.captchaVerifyParam || result?.CaptchaVerifyParam || '';
};

const buildCaptchaCallbackResult = (captchaVerifyParam: string): AliyunCaptchaCallbackResult => ({
    captchaResult: Boolean(captchaVerifyParam),
    bizResult: Boolean(captchaVerifyParam),
});

const AliyunCaptcha = forwardRef<AliyunCaptchaHandle, AliyunCaptchaProps>(({ disabled }, ref) => {
    const { t } = useLanguage();
    const [status, setStatus] = useState<CaptchaStatus>('loading');
    const [error, setError] = useState<string | null>(null);
    const instanceRef = useRef<AliyunCaptchaInstance | null>(null);
    const pendingRef = useRef<{
        resolve: (value: string) => void;
        reject: (error: Error) => void;
    } | null>(null);
    const ids = useMemo(() => {
        const suffix = Math.random().toString(36).slice(2, 10);
        return {
            element: `aliyun-captcha-${suffix}`,
            button: `aliyun-captcha-button-${suffix}`,
        };
    }, []);
    const config = useMemo(getAliyunCaptchaConfig, []);

    useEffect(() => {
        let cancelled = false;

        if (!config.enabled) {
            setStatus('disabled');
            return;
        }

        setStatus('loading');
        setError(null);
        loadCaptchaScript(config)
            .then(() => {
                if (cancelled) return;
                if (!window.initAliyunCaptcha) {
                    throw new Error('Captcha initializer is unavailable');
                }

                const options: AliyunCaptchaOptions = {
                    SceneId: config.sceneId,
                    prefix: config.prefix,
                    mode: 'popup',
                    element: `#${ids.element}`,
                    button: `#${ids.button}`,
                    region: config.region,
                    slideStyle: { width: 360, height: 40 },
                    getInstance: (instance) => {
                        instanceRef.current = instance;
                    },
                    captchaVerifyCallback: (result, callback) => {
                        const param = getCaptchaVerifyParam(result);
                        const verifyResult = buildCaptchaCallbackResult(param);
                        if (!param) {
                            pendingRef.current?.reject(new Error(t('login.captchaFailed')));
                        } else {
                            pendingRef.current?.resolve(param);
                        }
                        pendingRef.current = null;
                        callback?.(verifyResult);
                        return verifyResult;
                    },
                    onBizResultCallback: () => {
                        // 业务注册请求由 Login 组件在拿到 captchaVerifyParam 后发起。
                    },
                    success: (result) => {
                        const param = getCaptchaVerifyParam(result);
                        if (!param) {
                            pendingRef.current?.reject(new Error(t('login.captchaFailed')));
                        } else {
                            pendingRef.current?.resolve(param);
                        }
                        pendingRef.current = null;
                    },
                    fail: () => {
                        const nextError = new Error(t('login.captchaFailed'));
                        pendingRef.current?.reject(nextError);
                        pendingRef.current = null;
                        setError(nextError.message);
                    },
                    onError: (errorInfo) => {
                        const message = errorInfo?.msg || t('login.captchaFailed');
                        const nextError = new Error(message);
                        pendingRef.current?.reject(nextError);
                        pendingRef.current = null;
                        setError(message);
                    },
                    onClose: () => {
                        const nextError = new Error(t('login.captchaFailed'));
                        pendingRef.current?.reject(nextError);
                        pendingRef.current = null;
                    },
                };
                if (config.server?.length) {
                    options.server = config.server;
                }
                window.initAliyunCaptcha(options);
                setStatus('ready');
            })
            .catch((err) => {
                if (cancelled) return;
                setStatus('error');
                setError(err instanceof Error ? err.message : t('login.captchaLoadFailed'));
            });

        return () => {
            cancelled = true;
            pendingRef.current?.reject(new Error('Captcha was unmounted'));
            pendingRef.current = null;
        };
    }, [config, ids.button, ids.element, t]);

    useImperativeHandle(ref, () => ({
        verify: () => {
            if (!config.enabled) {
                return Promise.resolve('');
            }
            if (disabled) {
                return Promise.reject(new Error(t('login.captchaUnavailable')));
            }
            if (status !== 'ready') {
                return Promise.reject(new Error(error || t('login.captchaLoading')));
            }

            return new Promise<string>((resolve, reject) => {
                const triggerButton = document.getElementById(ids.button);
                if (!triggerButton) {
                    reject(new Error(t('login.captchaUnavailable')));
                    return;
                }

                const timeoutID = window.setTimeout(() => {
                    if (pendingRef.current) {
                        pendingRef.current = null;
                        reject(new Error(t('login.captchaFailed')));
                    }
                }, 120000);
                pendingRef.current = {
                    resolve: (value) => {
                        window.clearTimeout(timeoutID);
                        resolve(value);
                    },
                    reject: (err) => {
                        window.clearTimeout(timeoutID);
                        reject(err);
                    },
                };
                triggerButton.click();
            });
        },
        reset: () => {
            instanceRef.current?.refresh?.();
        },
    }), [config.enabled, disabled, error, ids.button, status, t]);

    if (!config.enabled) {
        return null;
    }

    const statusText = status === 'ready'
        ? t('login.captchaReady')
        : status === 'loading'
            ? t('login.captchaLoading')
            : error || t('login.captchaLoadFailed');

    return (
        <div className="rounded-lg border border-[#444] bg-[#262626] p-3">
            <div id={ids.element} className="min-h-0" />
            <button id={ids.button} type="button" className="hidden" aria-hidden="true" tabIndex={-1} />
            <div className="flex items-center gap-2 text-xs text-[#9E9E9E]">
                <span
                    className={`h-2 w-2 rounded-full ${status === 'ready' ? 'bg-primary' : status === 'error' ? 'bg-red-400' : 'bg-[#777]'}`}
                    aria-hidden="true"
                />
                <span>{statusText}</span>
            </div>
        </div>
    );
});

AliyunCaptcha.displayName = 'AliyunCaptcha';

export default AliyunCaptcha;
