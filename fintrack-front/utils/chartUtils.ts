import type { PredictionChartData } from '../types';

export const DEFAULT_CHART_MIN_WIDTH = 840;
export const DEFAULT_CHART_MAX_WIDTH = 2600;
export const DEFAULT_CHART_POINT_WIDTH = 64;

export const getPredictionChartPointCount = (chartData?: PredictionChartData | null): number => {
    if (!chartData) {
        return 0;
    }

    return Math.max(
        chartData.dates?.length || 0,
        chartData.actuals?.length || 0,
        chartData.predictions?.length || 0,
        chartData.proPredictions?.length || 0,
        chartData.actualChangePercents?.length || 0,
        chartData.predictedChangePercents?.length || 0,
        chartData.proPredictedChangePercents?.length || 0,
    );
};

export interface PredictionChartWidthOptions {
    minWidth?: number;
    maxWidth?: number;
    pointWidth?: number;
}

export const getPredictionChartMinWidth = (
    chartData?: PredictionChartData | null,
    options: PredictionChartWidthOptions = {},
): number => {
    const {
        minWidth = DEFAULT_CHART_MIN_WIDTH,
        maxWidth = DEFAULT_CHART_MAX_WIDTH,
        pointWidth = DEFAULT_CHART_POINT_WIDTH,
    } = options;
    const pointCount = getPredictionChartPointCount(chartData);

    return Math.min(
        maxWidth,
        Math.max(minWidth, pointCount * pointWidth),
    );
};
