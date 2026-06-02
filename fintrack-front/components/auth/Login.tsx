
import React, { useState, useEffect, useRef } from 'react';
import LogoIcon from '../icons/LogoIcon';
import GoogleIcon from '../icons/GoogleIcon';
import { useLanguage } from '../../contexts/LanguageContext';
import { authAPI } from '../../services/apiService';
import AliyunCaptcha, { AliyunCaptchaHandle } from './AliyunCaptcha';

interface LoginProps {
    onLogin: () => void;
    onBack?: () => void;
}

type FormType = 'login' | 'register';

interface InputFieldProps {
    id: string;
    label: string;
    type: string;
    placeholder: string;
    value: string;
    onChange: (value: string) => void;
    disabled?: boolean;
    isPassword?: boolean;
    required?: boolean;
}

const InputField: React.FC<InputFieldProps> = ({ id, label, type, placeholder, value, onChange, disabled, isPassword, required = true }) => {
    const [showPassword, setShowPassword] = useState(false);
    const inputType = isPassword ? (showPassword ? 'text' : 'password') : type;

    return (
        <div className="flex flex-col gap-1.5">
            <label htmlFor={id} className="text-sm font-medium text-white">
                {label}
            </label>
            <div className="relative">
                <input
                    id={id}
                    type={inputType}
                    className="flex h-12 w-full rounded-lg border border-[#444] bg-[#2a2a2a] px-3 py-2 text-sm text-white placeholder:text-[#666] focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-transparent disabled:cursor-not-allowed disabled:opacity-50 transition-all pr-10"
                    placeholder={placeholder}
                    value={value}
                    onChange={(e) => onChange(e.target.value)}
                    disabled={disabled}
                    required={required}
                />
                {isPassword && (
                    <button
                        type="button"
                        onClick={() => setShowPassword(!showPassword)}
                        className="absolute right-3 top-1/2 -translate-y-1/2 text-[#666] hover:text-white transition-colors flex items-center justify-center"
                    >
                        <span className="material-symbols-outlined text-[20px]">
                            {showPassword ? 'visibility_off' : 'visibility'}
                        </span>
                    </button>
                )}
            </div>
        </div>
    );
};

interface CheckboxFieldProps {
    id: string;
    label: string;
    checked: boolean;
    onChange: (checked: boolean) => void;
    disabled?: boolean;
}

const CheckboxField: React.FC<CheckboxFieldProps> = ({ id, label, checked, onChange, disabled }) => (
    <div className="flex items-center space-x-2">
        <input
            type="checkbox"
            id={id}
            className="h-4 w-4 rounded border-gray-600 bg-[#2a2a2a] text-[#91caae] focus:ring-[#91caae]/50 focus:ring-offset-[#1E1E1E]"
            checked={checked}
            onChange={(e) => onChange(e.target.checked)}
            disabled={disabled}
        />
        <label
            htmlFor={id}
            className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 text-[#9E9E9E]"
        >
            {label}
        </label>
    </div>
);

const Login: React.FC<LoginProps> = ({ onLogin, onBack }) => {
    const [formType, setFormType] = useState<FormType>('login');
    const { t } = useLanguage();

    const [username, setUsername] = useState('');
    const [email, setEmail] = useState('');
    const [password, setPassword] = useState('');
    const [activationCode, setActivationCode] = useState('');
    const [rememberPassword, setRememberPassword] = useState(false);
    const [isLoading, setIsLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const captchaRef = useRef<AliyunCaptchaHandle>(null);

    useEffect(() => {
        setError(null);
    }, [formType]);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setIsLoading(true);
        setError(null);

        try {
            if (formType === 'login') {
                await authAPI.login(email, password);
            } else {
                const captchaVerifyParam = await captchaRef.current?.verify();
                await authAPI.register(email, username, password, {
                    captchaVerifyParam,
                    activationCode,
                });
            }
            onLogin();
        } catch (err: any) {
            console.error('Auth error:', err);
            if (formType === 'register') {
                captchaRef.current?.reset();
            }
            setError(err.message || 'An error occurred during authentication');
        } finally {
            setIsLoading(false);
        }
    };

    const TabButton = ({ type, label }: { type: FormType; label: string }) => (
        <button
            className={`flex-1 pb-4 text-sm font-medium transition-all relative ${formType === type ? 'text-white' : 'text-[#666] hover:text-[#999]'
                }`}
            onClick={() => setFormType(type)}
        >
            {label}
            {formType === type && (
                <div className="absolute bottom-0 left-0 w-full h-0.5 bg-primary rounded-t-full" />
            )}
        </button>
    );

    return (
        <div className="relative flex min-h-screen w-full flex-col items-center justify-center bg-container-dark text-[#E0E0E0] p-4 sm:p-6 lg:p-8">
            {onBack && (
                <button
                    onClick={onBack}
                    className="absolute top-8 left-8 flex items-center gap-2 text-white/60 hover:text-white transition-colors"
                >
                    <span className="material-symbols-outlined">arrow_back</span>
                    <span>Back</span>
                </button>
            )}
            <div className="w-full max-w-4xl overflow-hidden rounded-xl bg-[#1E1E1E] shadow-2xl flex flex-col md:flex-row">
                <div className="w-full md:w-1/2 bg-background-dark p-8 sm:p-12 flex flex-col justify-center">
                    <div className="flex items-center gap-4 text-white mb-8">
                        <LogoIcon className="size-8" />
                        <h2 className="text-white text-2xl font-bold leading-tight tracking-[-0.015em]">MeetLife AI</h2>
                    </div>
                    <h1 className="text-white tracking-tight text-4xl font-bold leading-tight mb-4">{t('login.title')}</h1>
                    <p className="text-[#91caae] text-base font-normal leading-normal">
                        {t('login.subtitle')}
                    </p>
                </div>

                <div className="w-full md:w-1/2 p-8 sm:p-12 flex flex-col bg-[#1E1E1E]">
                    <div className="w-full">
                        <div className="flex border-b border-[#333]">
                            <TabButton type="login" label={t('login.loginTab')} />
                            <TabButton type="register" label={t('login.registerTab')} />
                        </div>
                    </div>

                    <div className="flex flex-col flex-1 mt-8">
                        <h3 className="text-white text-2xl font-bold leading-tight mb-6">
                            {formType === 'login' ? t('login.welcomeBack') : t('login.createAccount')}
                        </h3>

                        {error && (
                            <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg">
                                <p className="text-red-400 text-sm">{error}</p>
                            </div>
                        )}

                        <form className="flex flex-col gap-5" onSubmit={handleSubmit}>
                            {formType === 'register' && (
                                <InputField
                                    id="fullname"
                                    label={t('login.fullName')}
                                    type="text"
                                    placeholder={t('login.fullNamePlaceholder')}
                                    value={username}
                                    onChange={setUsername}
                                    disabled={isLoading}
                                />
                            )}
                            <InputField
                                id="email"
                                label={t('login.emailAddress')}
                                type="email"
                                placeholder={t('login.emailPlaceholder')}
                                value={email}
                                onChange={setEmail}
                                disabled={isLoading}
                            />
                            <InputField
                                id="password"
                                label={t('login.password')}
                                type="password"
                                placeholder={t('login.passwordPlaceholder')}
                                value={password}
                                onChange={setPassword}
                                disabled={isLoading}
                                isPassword={true}
                            />
                            {formType === 'register' && (
                                <InputField
                                    id="activationCode"
                                    label={t('login.activationCode')}
                                    type="text"
                                    placeholder={t('login.activationCodePlaceholder')}
                                    value={activationCode}
                                    onChange={setActivationCode}
                                    disabled={isLoading}
                                    required={false}
                                />
                            )}

                            {formType === 'login' && (
                                <div className="flex items-center justify-between -mt-2">
                                    <CheckboxField
                                        id="rememberPassword"
                                        label={t('login.rememberPassword')}
                                        checked={rememberPassword}
                                        onChange={setRememberPassword}
                                        disabled={isLoading}
                                    />
                                    <a className="text-sm text-primary hover:underline" href="#">{t('login.forgotPassword')}</a>
                                </div>
                            )}

                            {formType === 'register' && (
                                <div className="-mt-2">
                                    <AliyunCaptcha ref={captchaRef} disabled={isLoading} />
                                </div>
                            )}

                            <button
                                className="flex items-center justify-center whitespace-nowrap rounded-lg text-sm font-bold h-12 px-6 bg-primary text-black mt-4 hover:bg-opacity-90 transition-colors focus:outline-none focus:ring-2 focus:ring-primary/50 focus:ring-offset-2 focus:ring-offset-[#1E1E1E] disabled:opacity-50 disabled:cursor-not-allowed"
                                type="submit"
                                disabled={isLoading}
                            >
                                {isLoading ? (
                                    <div className="flex items-center gap-2">
                                        <div className="w-4 h-4 border-2 border-black border-t-transparent rounded-full animate-spin"></div>
                                        {formType === 'login' ? t('login.loggingIn') : t('login.registering')}
                                    </div>
                                ) : (
                                    formType === 'login' ? t('login.signIn') : t('login.register')
                                )}
                            </button>

                            <div className="relative flex items-center py-2">
                                <div className="flex-grow border-t border-[#444]"></div>
                                <span className="flex-shrink mx-4 text-xs text-[#9E9E9E]">{t('login.or')}</span>
                                <div className="flex-grow border-t border-[#444]"></div>
                            </div>

                            <button className="flex items-center justify-center whitespace-nowrap rounded-lg text-sm font-medium h-12 px-6 bg-[#2a2a2a] text-white border border-[#444] hover:bg-[#333] transition-colors focus:outline-none focus:ring-2 focus:ring-primary/50 focus:ring-offset-2 focus:ring-offset-[#1E1E1E] gap-2 disabled:opacity-50 disabled:cursor-not-allowed" type="button" onClick={onLogin} disabled={isLoading}>
                                <GoogleIcon className="h-5 w-5" />
                                {t('login.continueWithGoogle')}
                            </button>
                        </form>
                    </div>
                </div>
            </div>
        </div>
    );
};

export default Login;
