
import { StockData, View } from './types';

export const INITIAL_STOCKS: StockData[] = [
  {
    symbol: 'AAPL',
    companyName: 'Apple Inc.',
    currentPrice: 190.45,
    changePercent: 0.81,
  },
  {
    symbol: 'GOOGL',
    companyName: 'Alphabet Inc.',
    currentPrice: 175.22,
    changePercent: -0.58,
  },
  {
    symbol: 'TSLA',
    companyName: 'Tesla, Inc.',
    currentPrice: 182.01,
    changePercent: 2.10,
  },
  {
    symbol: 'NVDA',
    companyName: 'NVIDIA Corporation',
    currentPrice: 905.54,
    changePercent: 1.72,
  },
  {
    symbol: 'AMZN',
    companyName: 'Amazon.com, Inc.',
    currentPrice: 185.30,
    changePercent: -0.25,
  },
    {
    symbol: 'META',
    companyName: 'Meta Platforms, Inc.',
    currentPrice: 470.90,
    changePercent: 1.15,
  },
];

export const NAVIGATION_ITEMS: { id: View; label: string; icon: string; }[] = [
    { id: 'mtfAgent', label: 'MTF Agent', icon: 'smart_toy' },
    { id: 'dashboard', label: 'Predicted Trends', icon: 'insights' },
    { id: 'watchlist', label: 'Watchlist', icon: 'star' },
    { id: 'portfolio', label: 'Portfolio', icon: 'pie_chart' },
    { id: 'news', label: 'Finance News', icon: 'newspaper' },
    { id: 'pricing', label: 'VIP Membership', icon: 'workspace_premium'},
];
