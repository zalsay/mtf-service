import React, { useState } from 'react';
import { StrategyParams } from '../../types';
import { strategyAPI } from '../../services/apiService';
import { useLanguage } from '../../contexts/LanguageContext';

interface CreateStrategyModalProps {
    isOpen: boolean;
    onClose: () => void;
    onSuccess?: () => void;
}

const CreateStrategyModal: React.FC<CreateStrategyModalProps> = ({ isOpen, onClose, onSuccess }) => {
    const { t, language } = useLanguage();
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    // Strategy Params State
    const [formData, setFormData] = useState<StrategyParams>({
        unique_key: '',
        name: '',
        buy_threshold_pct: 1.5,
        sell_threshold_pct: -1.0,
        initial_cash: 100000,
        enable_rebalance: true,
        max_position_pct: 0.95,
        min_position_pct: 0.1,
        slope_position_per_pct: 0.2,
        rebalance_tolerance_pct: 0.05,
        trade_fee_rate: 0.001,
        take_profit_threshold_pct: 15.0,
        take_profit_sell_frac: 0.5,
    });

    if (!isOpen) return null;

    const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        const { name, value, type, checked } = e.target;
        setFormData(prev => ({
            ...prev,
            [name]: type === 'checkbox' ? checked : (type === 'number' ? parseFloat(value) : value)
        }));
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError(null);
        setLoading(true);
        
        try {
            // Generate a unique key for the template
            const templateKey = `tpl_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
            
            const payload = { ...formData, unique_key: templateKey };
            await strategyAPI.saveParams(payload);

            if (onSuccess) onSuccess();
            onClose();
        } catch (err: any) {
            setError(err.message || "Failed to create strategy");
        } finally {
            setLoading(false);
        }
    };

    const labels: Record<string, string> = {
        name: language === 'zh' ? '策略名称' : 'Strategy Name',
        buy_threshold_pct: language === 'zh' ? '买入阈值(%)' : 'Buy Threshold(%)',
        sell_threshold_pct: language === 'zh' ? '卖出阈值(%)' : 'Sell Threshold(%)',
        initial_cash: language === 'zh' ? '初始资金' : 'Initial Cash',
        enable_rebalance: language === 'zh' ? '启用动态平衡' : 'Enable Rebalance',
        max_position_pct: language === 'zh' ? '最大仓位比例' : 'Max Position %',
        min_position_pct: language === 'zh' ? '最小仓位比例' : 'Min Position %',
        slope_position_per_pct: language === 'zh' ? '仓位斜率(每%)' : 'Slope Position/%',
        rebalance_tolerance_pct: language === 'zh' ? '平衡容差(%)' : 'Rebalance Tolerance(%)',
        trade_fee_rate: language === 'zh' ? '交易费率' : 'Trade Fee Rate',
        take_profit_threshold_pct: language === 'zh' ? '止盈阈值(%)' : 'Take Profit Threshold(%)',
        take_profit_sell_frac: language === 'zh' ? '止盈卖出比例' : 'Take Profit Sell Fraction',
    };

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4">
            <div className="w-full max-w-2xl rounded-2xl bg-[#1E1E1E] border border-white/10 shadow-2xl max-h-[90vh] overflow-y-auto">
                <div className="flex items-center justify-between border-b border-white/10 p-6">
                    <h2 className="text-xl font-bold text-white">
                        {language === 'zh' ? '添加新策略' : 'Add New Strategy'}
                    </h2>
                    <button onClick={onClose} className="rounded-lg p-2 text-white/60 hover:bg-white/5 hover:text-white">
                        <span className="material-symbols-outlined">close</span>
                    </button>
                </div>

                <form onSubmit={handleSubmit} className="p-6 space-y-6">
                    {error && (
                        <div className="p-3 rounded bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
                            {error}
                        </div>
                    )}

                    {/* Strategy Params Section */}
                    <div className="bg-white/5 rounded-xl p-4 border border-white/10 space-y-4">
                        <h3 className="text-sm font-bold text-white/80 uppercase tracking-wider border-b border-white/10 pb-2">
                            {language === 'zh' ? '配置参数' : 'Configure Parameters'}
                        </h3>
                        
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            {Object.keys(formData).map((key) => {
                                if (key === 'unique_key' || key === 'user_id') return null;
                                const isBool = typeof (formData as any)[key] === 'boolean';
                                const isString = typeof (formData as any)[key] === 'string';
                                
                                return (
                                    <div key={key} className={isBool || isString ? "col-span-2 flex items-center gap-3" : "space-y-2"}>
                                        {isBool ? (
                                            <label htmlFor={`create_${key}`} className="flex cursor-pointer items-center gap-3">
                                                <input
                                                    type="checkbox"
                                                    name={key}
                                                    id={`create_${key}`}
                                                    checked={(formData as any)[key]}
                                                    onChange={handleChange}
                                                    className="peer sr-only"
                                                />
                                                <span className="flex h-5 w-5 items-center justify-center rounded-md border border-white/20 bg-black/35 text-transparent transition-colors peer-checked:border-primary/70 peer-checked:bg-black/60 peer-checked:text-primary peer-focus-visible:ring-2 peer-focus-visible:ring-primary/50">
                                                    <span className="material-symbols-outlined text-[16px] leading-none">check</span>
                                                </span>
                                                <span className="text-sm text-white/80 font-medium">
                                                    {labels[key] || key}
                                                </span>
                                            </label>
                                        ) : isString ? (
                                            <div className="w-full space-y-2">
                                                <label htmlFor={`create_${key}`} className="text-xs text-white/60 uppercase font-semibold tracking-wider">
                                                    {labels[key] || key}
                                                </label>
                                                <input
                                                    type="text"
                                                    name={key}
                                                    id={`create_${key}`}
                                                    value={(formData as any)[key]}
                                                    onChange={handleChange}
                                                    className="w-full rounded-lg bg-white/5 border border-white/10 px-4 py-2.5 text-white focus:border-primary focus:outline-none focus:ring-1 focus:ring-primary transition-all"
                                                    placeholder={language === 'zh' ? '输入策略名称' : 'Enter strategy name'}
                                                />
                                            </div>
                                        ) : (
                                            <>
                                                <label htmlFor={`create_${key}`} className="text-xs text-white/60 uppercase font-semibold tracking-wider">
                                                    {labels[key] || key}
                                                </label>
                                                <input
                                                    type="number"
                                                    step="0.0001"
                                                    name={key}
                                                    id={`create_${key}`}
                                                    value={(formData as any)[key]}
                                                    onChange={handleChange}
                                                    className="w-full rounded-lg bg-white/5 border border-white/10 px-4 py-2.5 text-white focus:border-primary focus:outline-none focus:ring-1 focus:ring-primary transition-all"
                                                />
                                            </>
                                        )}
                                    </div>
                                );
                            })}
                        </div>
                    </div>

                    <div className="flex justify-end gap-3 pt-4 border-t border-white/10">
                        <button
                            type="button"
                            onClick={onClose}
                            className="px-6 py-2.5 rounded-lg text-sm font-medium text-white/80 hover:bg-white/5 hover:text-white transition-colors"
                        >
                            {language === 'zh' ? '取消' : 'Cancel'}
                        </button>
                        <button
                            type="submit"
                            disabled={loading}
                            className="px-6 py-2.5 rounded-lg bg-primary text-background-dark text-sm font-bold hover:opacity-90 transition-opacity disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
                        >
                            {loading && <span className="material-symbols-outlined animate-spin text-lg">progress_activity</span>}
                            {language === 'zh' ? '创建策略' : 'Create Strategy'}
                        </button>
                    </div>
                </form>
            </div>
        </div>
    );
};

export default CreateStrategyModal;
