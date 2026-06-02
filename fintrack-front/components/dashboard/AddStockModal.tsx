import React, { useEffect, useRef, useState } from 'react';
import { useLanguage } from '../../contexts/LanguageContext';
import { stocksAPI } from '../../services/apiService';

interface AddStockModalProps {
    isOpen: boolean;
    onClose: () => void;
    onAdd: (symbol: string, type: 1 | 2) => Promise<void>;
}

const AddStockModal: React.FC<AddStockModalProps> = ({ isOpen, onClose, onAdd }) => {
    const { t } = useLanguage();
    const [type, setType] = useState<1 | 2>(1);
    const [exchange, setExchange] = useState<'sh' | 'sz'>('sh');
    const [stockCode, setStockCode] = useState('');
    const [isLoading, setIsLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [resolvedName, setResolvedName] = useState<string | null>(null);
    const lookupKeyRef = useRef<string>('');

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError(null);

        // Validate stock code
        if (!stockCode || stockCode.length !== 6) {
            setError(t('addStock.errorLength'));
            return;
        }

        if (!/^\d{6}$/.test(stockCode)) {
            setError(t('addStock.errorNumeric'));
            return;
        }

        setIsLoading(true);
        try {
            const fullSymbol = `${exchange}${stockCode}`;
            await onAdd(fullSymbol, type);
            // Reset form on success
            setStockCode('');
            setExchange('sh');
            onClose();
        } catch (err: any) {
            const msg = err.message || t('addStock.errorGeneric');
            setError(msg);
        } finally {
            setIsLoading(false);
        }
    };

    const handleClose = () => {
        if (!isLoading) {
            setStockCode('');
            setExchange('sh');
            setType(1);
            setError(null);
            onClose();
        }
    };

    useEffect(() => {
        setResolvedName(null);
        setError(null);
        if (stockCode && stockCode.length === 6) {
            const queryKey = type === 1 ? stockCode : `${exchange}${stockCode}`;
            const myKey = `${type}-${queryKey}`;
            lookupKeyRef.current = myKey;
            stocksAPI.lookupName(queryKey, type)
                .then(res => {
                    if (lookupKeyRef.current !== myKey) return;
                    setResolvedName(res.name);
                    setError(null);
                })
                .catch(err => {
                    if (lookupKeyRef.current !== myKey) return;
                    const msg = err?.message || t('addStock.errorNotFound');
                    setResolvedName(null);
                    setError(msg);
                });
        }
    }, [stockCode, exchange, type]);

    if (!isOpen) return null;

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm">
            <div className="w-full max-w-md bg-card-dark rounded-xl shadow-2xl border border-white/10 overflow-hidden">
                {/* Header */}
                <div className="px-6 py-4 border-b border-white/10 flex items-center justify-between">
                    <h2 className="text-xl font-bold text-white">{t('addStock.title')}</h2>
                    <button
                        onClick={handleClose}
                        disabled={isLoading}
                        className="text-white/60 hover:text-white transition-colors disabled:opacity-50"
                    >
                        <svg className="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                        </svg>
                    </button>
                </div>

                {/* Body */}
                <form onSubmit={handleSubmit} className="p-6 space-y-4">
                    {/* Type Selection */}
                    <div className="flex flex-col">
                        <label className="text-white/80 text-sm font-medium mb-2">{t('addStock.type')}</label>
                        <div className="flex gap-3">
                            <button
                                type="button"
                                onClick={() => setType(1)}
                                disabled={isLoading}
                                className={`flex-1 px-4 py-3 rounded-lg font-medium transition-colors ${type === 1
                                        ? 'bg-primary text-background-dark'
                                        : 'bg-white/5 text-white/60 hover:bg-white/10 hover:text-white'
                                    } disabled:opacity-50`}
                            >
                                {t('addStock.typeStock')}
                            </button>
                            <button
                                type="button"
                                onClick={() => setType(2)}
                                disabled={isLoading}
                                className={`flex-1 px-4 py-3 rounded-lg font-medium transition-colors ${type === 2
                                        ? 'bg-primary text-background-dark'
                                        : 'bg-white/5 text-white/60 hover:bg-white/10 hover:text-white'
                                    } disabled:opacity-50`}
                            >
                                {t('addStock.typeEtf')}
                            </button>
                        </div>
                    </div>

                    {/* Exchange Selection */}
                    <div className="flex flex-col">
                        <label className="text-white/80 text-sm font-medium mb-2">{t('addStock.exchange')}</label>
                        <div className="flex gap-3">
                            <button
                                type="button"
                                onClick={() => setExchange('sh')}
                                disabled={isLoading}
                                className={`flex-1 px-4 py-3 rounded-lg font-medium transition-colors ${exchange === 'sh'
                                        ? 'bg-primary text-background-dark'
                                        : 'bg-white/5 text-white/60 hover:bg-white/10 hover:text-white'
                                    } disabled:opacity-50`}
                            >
                                {t('addStock.exchangeSh')}
                            </button>
                            <button
                                type="button"
                                onClick={() => setExchange('sz')}
                                disabled={isLoading}
                                className={`flex-1 px-4 py-3 rounded-lg font-medium transition-colors ${exchange === 'sz'
                                        ? 'bg-primary text-background-dark'
                                        : 'bg-white/5 text-white/60 hover:bg-white/10 hover:text-white'
                                    } disabled:opacity-50`}
                            >
                                {t('addStock.exchangeSz')}
                            </button>
                        </div>
                    </div>

                    {/* Stock Code Input */}
                    <div className="flex flex-col">
                        <label className="text-white/80 text-sm font-medium mb-2">
                            {t('addStock.stockCode')}
                            <span className="text-white/40 ml-2">{t('addStock.stockCodeHint')}</span>
                        </label>
                        <input
                            type="text"
                            value={stockCode}
                            onChange={(e) => setStockCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                            placeholder={t('addStock.stockCodePlaceholder')}
                            disabled={isLoading}
                            className="w-full px-4 py-3 bg-white/5 border border-white/10 rounded-lg text-white placeholder:text-white/40 focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-transparent disabled:opacity-50"
                            maxLength={6}
                            autoFocus
                        />
                        <p className="text-white/40 text-xs mt-2">
                            {t('addStock.fullCode')} <span className="text-primary font-mono">{exchange}{stockCode || '______'}</span>{resolvedName ? <span className="ml-2 text-white/60">{resolvedName}</span> : null}
                        </p>
                    </div>

                    {/* Error Message */}
                    {error && (!resolvedName || (error && error.includes('duplicate symbol'))) && (
                        <div className="p-2 bg-red-500/10 border border-red-500/20 rounded-lg">
                            <p className="text-red-400 text-xs">
                                {(() => {
                                    if (error.includes('symbol not found')) {
                                        return t('addStock.errorNotFound');
                                    }
                                    if (error.includes('User not authenticated') || error.includes('Unauthorized')) {
                                        return t('addStock.errorAuth');
                                    }
                                    if (error.includes('duplicate symbol')) {
                                        return t('addStock.errorDuplicate');
                                    }
                                    return error;
                                })()}
                            </p>
                        </div>
                    )}

                    {/* Submit Button */}
                    <button
                        type="submit"
                        disabled={isLoading || stockCode.length !== 6}
                        className="w-full py-3 px-4 bg-primary text-background-dark font-bold rounded-lg hover:opacity-90 transition-opacity disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center"
                    >
                        {isLoading ? (
                            <div className="w-5 h-5 border-2 border-background-dark/30 border-t-background-dark rounded-full animate-spin"></div>
                        ) : (
                            t('addStock.title')
                        )}
                    </button>
                </form>
            </div>
        </div>
    );
};

export default AddStockModal;
