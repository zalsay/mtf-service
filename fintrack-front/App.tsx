
import React, { useState, useEffect, useCallback } from 'react';
import Login from './components/auth/Login';
import LandingPage from './components/landing/LandingPage';
import Sidebar from './components/layout/Sidebar';
import Dashboard from './components/dashboard/Dashboard';
import Watchlist from './components/watchlist/Watchlist';
import Pricing from './components/pricing/Pricing';
import Portfolio from './components/portfolio/Portfolio';
import FinanceNews from './components/news/FinanceNews';
import Settings from './components/settings/Settings';
import Admin from './components/admin/Admin';
import MTFAgentDrawer from './components/mtfAgent/MTFAgentDrawer';
import MTFAgentPage from './components/mtfAgent/MTFAgentPage';
import { View, StockData } from './types';
import { getStockPredictions } from './services/geminiService';
import { INITIAL_STOCKS } from './constants';
import { LanguageProvider, useLanguage } from './contexts/LanguageContext';
import { authAPI, clearAuthToken, onAuthRequired } from './services/apiService';
import ErrorBoundary from './components/ErrorBoundary';

const AppContent: React.FC = () => {
    const { t, language } = useLanguage();
    const [isAuthenticated, setIsAuthenticated] = useState<boolean>(false);
    const [isAdmin, setIsAdmin] = useState<boolean>(false);
    const [isCheckingAuth, setIsCheckingAuth] = useState<boolean>(true);
    const [currentView, setCurrentView] = useState<View>('dashboard');
    const [lastContentView, setLastContentView] = useState<View>('dashboard');
    const [stocks, setStocks] = useState<StockData[]>(INITIAL_STOCKS);
    const [isLoading, setIsLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);
    const [isMobileViewport, setIsMobileViewport] = useState<boolean>(() => (
        typeof window !== 'undefined' ? window.innerWidth < 1024 : false
    ));

    const [showLogin, setShowLogin] = useState<boolean>(false);
    const [isDemoMode, setIsDemoMode] = useState<boolean>(false);

    const handleAuthError = useCallback(() => {
        clearAuthToken();
        setIsAuthenticated(false);
        setIsAdmin(false);
        setIsDemoMode(false);
        setCurrentView('dashboard');
        setLastContentView('dashboard');
        setShowLogin(true);
        setError(null);
    }, []);

    // 检查用户是否已登录
    useEffect(() => {
        const checkAuth = async () => {
            const token = localStorage.getItem('authToken');  // 修复：使用正确的 key
            if (token) {
                try {
                    // 验证token是否有效
                    const profile = await authAPI.getProfile();
                    setIsAdmin(Boolean(profile.is_admin));
                    setIsAuthenticated(true);
                } catch {
                    handleAuthError();
                }
            }
            setIsCheckingAuth(false);
        };
        checkAuth();
    }, [handleAuthError]);

    useEffect(() => onAuthRequired(handleAuthError), [handleAuthError]);

    const fetchPredictions = useCallback(async () => {
        setIsLoading(true);
        setError(null);
        try {
            const stockSymbols = INITIAL_STOCKS.map(s => s.symbol);
            const predictions = await getStockPredictions(stockSymbols);
            setStocks(prevStocks => prevStocks.map(stock => ({
                ...stock,
                prediction: predictions[stock.symbol] || stock.prediction,
            })));
        } catch (err: any) {
            // 如果是授权错误，自动跳转登录
            if (err.message && (
                err.message.includes('Authorization header required') || 
                err.message.includes('401') ||
                err.message.includes('Unauthorized')
            )) {
                handleAuthError();
            } else {
                setError(err.message);
                console.error(err);
            }
        } finally {
            setIsLoading(false);
        }
    }, [handleAuthError]);

    useEffect(() => {
        if (isAuthenticated || isDemoMode) {
            fetchPredictions();
        }
    }, [isAuthenticated, isDemoMode, fetchPredictions]);

    useEffect(() => {
        const handleResize = () => {
            setIsMobileViewport(window.innerWidth < 1024);
        };

        handleResize();
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, []);

    const handleLogin = () => {
        setCurrentView('dashboard');
        setLastContentView('dashboard');
        setIsAuthenticated(true);
        authAPI.getProfile()
            .then((profile) => setIsAdmin(Boolean(profile.is_admin)))
            .catch(() => setIsAdmin(false));
    };

    const handleLogout = async () => {
        try {
            await authAPI.logout();
        } catch (error) {
            console.error('Logout error:', error);
        } finally {
            clearAuthToken();
            setIsAuthenticated(false);
            setIsAdmin(false);
            setIsDemoMode(false); // Exit demo mode on logout
            setCurrentView('dashboard'); // Reset default view for next login
            setLastContentView('dashboard');
            setShowLogin(false); // Reset to landing page on logout
        }
    };

    const handleViewChange = useCallback((view: View) => {
        if (view !== 'settings') {
            setLastContentView(view);
        }
        setCurrentView(view);
    }, []);

    const handleCloseSettings = useCallback(() => {
        setCurrentView(lastContentView === 'settings' ? 'dashboard' : lastContentView);
    }, [lastContentView]);

    const renderView = (view: View) => {
        switch (view) {
            case 'mtfAgent':
                return (
                    <MTFAgentPage
                        onAuthError={handleAuthError}
                        onOpenSettings={() => handleViewChange('settings')}
                    />
                );
            case 'dashboard':
                return <Dashboard 
                    stocks={stocks} 
                    isLoading={isLoading} 
                    error={error} 
                    onAuthError={handleAuthError}
                />;
            case 'watchlist':
                return <Watchlist 
                    initialStocks={INITIAL_STOCKS} 
                    onAuthError={handleAuthError}
                />;
            case 'pricing':
                return <Pricing />;
            case 'portfolio':
                return <Portfolio onAuthError={handleAuthError} />;
            case 'news':
                return <FinanceNews onAuthError={handleAuthError} />;
            case 'settings':
                return <Settings onLogout={handleLogout} isDemoMode={isDemoMode} />;
            case 'admin':
                return isAdmin ? <Admin /> : (
                    <div className="rounded-2xl border border-red-400/20 bg-red-500/10 p-6 text-red-100">
                        <h2 className="text-2xl font-black">{t('admin.noAccessTitle')}</h2>
                        <p className="mt-2 text-sm text-red-100/75">{t('admin.noAccessDesc')}</p>
                    </div>
                );
            default:
                return <div className="text-center p-8 bg-card-dark rounded-lg">
                    <h2 className="text-2xl font-bold">{t('common.comingSoon')}</h2>
                    <p className="text-white/60 mt-2">{t('common.comingSoonDesc')}</p>
                </div>;
        }
    };

    const mainView = isMobileViewport && currentView === 'settings'
        ? (lastContentView === 'settings' ? 'dashboard' : lastContentView)
        : currentView;
    const mobileChromeBgClass = language === 'zh' ? 'bg-[#261817]' : 'bg-[#16211d]';
    const mobileNavItems: { id: View; icon: string; labelKey: string }[] = [
        { id: 'dashboard', icon: 'insights', labelKey: 'nav.dashboard' },
        { id: 'watchlist', icon: 'star', labelKey: 'nav.watchlist' },
        { id: 'portfolio', icon: 'pie_chart', labelKey: 'nav.portfolio' },
        { id: 'news', icon: 'newspaper', labelKey: 'nav.news' },
        { id: 'pricing', icon: 'workspace_premium', labelKey: 'nav.pricing' },
        ...(isAdmin ? [{ id: 'admin' as View, icon: 'admin_panel_settings', labelKey: 'nav.admin' }] : []),
        { id: 'settings', icon: 'settings', labelKey: 'nav.settings' },
    ].filter(item => !isDemoMode || item.id !== 'mtfAgent');

    // 显示加载画面，等待token验证完成
    if (isCheckingAuth) {
        return (
            <div className="flex items-center justify-center min-h-screen bg-background-dark">
                <div className="text-center">
                    <div className="w-16 h-16 border-4 border-primary border-t-transparent rounded-full animate-spin mx-auto mb-4"></div>
                    <p className="text-white/60">{t('auth.checking')}</p>
                </div>
            </div>
        );
    }

    if (!isAuthenticated && !isDemoMode) {
        if (showLogin) {
            return <Login onLogin={handleLogin} onBack={() => setShowLogin(false)} />;
        }
        return <LandingPage onLogin={() => setShowLogin(true)} onRegister={() => setShowLogin(true)} onDemo={() => setIsDemoMode(true)} />;
    }

    return (
        <div className="flex min-h-screen">
            <Sidebar currentView={currentView} setCurrentView={handleViewChange} onLogout={handleLogout} isDemoMode={isDemoMode} isAdmin={isAdmin} />

            {isMobileViewport && currentView === 'settings' && (
                <div className="fixed inset-x-0 top-0 bottom-16 z-40 lg:hidden">
                    <button
                        type="button"
                        aria-label={t('settings.close')}
                        onClick={handleCloseSettings}
                        className="absolute inset-0 bg-black/55 backdrop-blur-[2px]"
                    />
                    <div className={`absolute inset-x-0 bottom-0 max-h-full overflow-y-auto overscroll-contain rounded-t-[28px] border border-white/10 px-4 pb-6 pt-5 shadow-[0_-24px_48px_rgba(0,0,0,0.34)] ${mobileChromeBgClass}`}>
                        <div className="mx-auto mb-4 h-1.5 w-12 rounded-full bg-white/15" />
                        <Settings onLogout={handleLogout} isDemoMode={isDemoMode} />
                    </div>
                </div>
            )}

            {/* Mobile Navigation */}
            <div className={`lg:hidden fixed bottom-0 left-0 right-0 border-t border-white/10 z-50 safe-area-inset-bottom ${mobileChromeBgClass}`}>
                <div className="flex justify-around items-center h-16">
                    {mobileNavItems.map(item => (
                        <button
                            key={item.id}
                            onClick={() => handleViewChange(item.id)}
                            className={`flex flex-col items-center justify-center py-1 px-2 w-full h-full transition-colors ${currentView === item.id ? 'text-primary' : 'text-white/60 hover:text-white'
                                }`}
                        >
                            <span className="material-symbols-outlined text-lg" style={{ fontVariationSettings: currentView === item.id ? "'FILL' 1" : "" }}>
                                {item.icon}
                            </span>
                            <span className="mt-0.5 truncate text-[8px] leading-tight sm:text-[9px]">
                                {t(item.labelKey)}
                            </span>
                        </button>
                    ))}
                </div>
            </div>

            <main className="flex-1 p-3 sm:p-6 lg:p-8 pb-20 sm:pb-24 lg:pb-8 min-h-screen lg:min-h-0">
                <div className={mainView === 'settings' ? 'max-w-full overflow-visible' : 'max-w-full overflow-x-auto'}>
                    {renderView(mainView)}
                </div>
            </main>

            {!isDemoMode && currentView !== 'mtfAgent' && (
                <MTFAgentDrawer
                    onAuthError={handleAuthError}
                    onOpenSettings={() => handleViewChange('settings')}
                />
            )}
        </div>
    );
};

const App: React.FC = () => {
    return (
        <LanguageProvider>
            <ErrorBoundary>
                <AppContent />
            </ErrorBoundary>
        </LanguageProvider>
    );
};

export default App;
