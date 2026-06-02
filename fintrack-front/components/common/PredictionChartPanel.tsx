import React from 'react';
import type { PredictionChartData } from '../../types';
import { getPredictionChartMinWidth, type PredictionChartWidthOptions } from '../../utils/chartUtils';
import PredictionChart, {
    type PredictionChartDetailMode,
    type PredictionChartMarker,
    type PredictionChartMode,
    type PredictionChartTheme,
} from './PredictionChart';

interface PredictionChartPanelProps extends PredictionChartWidthOptions {
    change: number;
    chartData?: PredictionChartData;
    currentPrice?: number;
    startPrice?: number;
    mode?: PredictionChartMode;
    theme?: PredictionChartTheme;
    markers?: PredictionChartMarker[];
    detailMode?: PredictionChartDetailMode;
    scrollable?: boolean;
    fitToContainer?: boolean;
    actualLabel?: string;
    className?: string;
    chartClassName?: string;
}

const PredictionChartPanel: React.FC<PredictionChartPanelProps> = ({
    change,
    chartData,
    currentPrice,
    startPrice,
    mode,
    theme,
    markers,
    detailMode,
    scrollable = false,
    fitToContainer,
    actualLabel,
    className = 'h-full',
    chartClassName = 'h-full',
    minWidth,
    maxWidth,
    pointWidth,
}) => {
    const chart = (
        <PredictionChart
            change={change}
            chartData={chartData}
            currentPrice={currentPrice}
            actualLabel={actualLabel}
            detailMode={detailMode}
            fitToContainer={fitToContainer ?? scrollable}
            markers={markers}
            mode={mode}
            startPrice={startPrice}
            theme={theme}
        />
    );

    if (!scrollable) {
        return (
            <div className={className}>
                {chart}
            </div>
        );
    }

    const chartMinWidth = getPredictionChartMinWidth(chartData, {
        minWidth,
        maxWidth,
        pointWidth,
    });

    return (
        <div className={`min-w-0 overflow-x-auto overflow-y-hidden pb-3 ${className}`.trim()}>
            <div
                className={`flex-none ${chartClassName}`.trim()}
                style={{
                    minWidth: `${chartMinWidth}px`,
                    width: '100%',
                }}
            >
                {chart}
            </div>
        </div>
    );
};

export default PredictionChartPanel;
