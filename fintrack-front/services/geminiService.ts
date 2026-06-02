
import { GoogleGenAI, Type } from "@google/genai";
import { StockPrediction } from '../types';

// Mock environment for disabling Gemini API
const API_KEY = null; // Force API key to null to disable actual calls

const ai = new GoogleGenAI({ apiKey: 'dummy-key' });

const stockPredictionSchema = {
    type: Type.OBJECT,
    properties: {
        predictions: {
            type: Type.ARRAY,
            description: "An array of stock predictions.",
            items: {
                type: Type.OBJECT,
                properties: {
                    symbol: {
                        type: Type.STRING,
                        description: "The stock ticker symbol.",
                    },
                    predicted_high: {
                        type: Type.NUMBER,
                        description: "The predicted high price for the next trading period.",
                    },
                    predicted_low: {
                        type: Type.NUMBER,
                        description: "The predicted low price for the next trading period.",
                    },
                    confidence: {
                        type: Type.NUMBER,
                        description: "A confidence score for the prediction, from 0 to 100.",
                    },
                    sentiment: {
                        type: Type.STRING,
                        description: "The overall market sentiment for the stock.",
                        enum: ['Bullish', 'Bearish', 'Neutral'],
                    },
                    analysis: {
                        type: Type.STRING,
                        description: "A brief, one-sentence analysis supporting the prediction."
                    }
                },
                required: ["symbol", "predicted_high", "predicted_low", "confidence", "sentiment", "analysis"]
            }
        }
    },
    required: ["predictions"],
};


export const getStockPredictions = async (stockSymbols: string[]): Promise<Record<string, StockPrediction>> => {
    // Always return mock predictions as the API is disabled
    // await new Promise(resolve => setTimeout(resolve, 1500)); // simulate network delay
    const mockPredictions: Record<string, StockPrediction> = {};
    stockSymbols.forEach(symbol => {
        const isBullish = Math.random() > 0.4;
        mockPredictions[symbol] = {
            predicted_high: Math.random() * 50 + 180,
            predicted_low: Math.random() * 20 + 160,
            confidence: Math.floor(Math.random() * 25) + 75,
            sentiment: isBullish ? 'Bullish' : 'Bearish',
            analysis: `[MOCK] Based on recent market trends, ${symbol} shows potential for short-term ${isBullish ? 'growth' : 'volatility'}.`,
        };
    });
    return mockPredictions;

    /* 
    // Original Gemini API implementation disabled
    if (!process.env.API_KEY) {
        // Return mock predictions if API key is not available
        await new Promise(resolve => setTimeout(resolve, 1500)); // simulate network delay
        const mockPredictions: Record<string, StockPrediction> = {};
        stockSymbols.forEach(symbol => {
            const isBullish = Math.random() > 0.4;
            mockPredictions[symbol] = {
                predicted_high: Math.random() * 50 + 180,
                predicted_low: Math.random() * 20 + 160,
                confidence: Math.floor(Math.random() * 25) + 75,
                sentiment: isBullish ? 'Bullish' : 'Bearish',
                analysis: `Based on recent market trends, ${symbol} shows potential for short-term ${isBullish ? 'growth' : 'volatility'}.`,
            };
        });
        return mockPredictions;
    }

    try {
        const prompt = `
            Analyze the following stock symbols: ${stockSymbols.join(', ')}.
            For each stock, provide a financial prediction for the next trading period.
            Include a predicted high price, predicted low price, a confidence score (0-100), market sentiment (Bullish, Bearish, or Neutral), and a brief one-sentence analysis.
            Do not use markdown.
        `;

        const response = await ai.models.generateContent({
            model: "gemini-2.5-flash",
            contents: prompt,
            config: {
                responseMimeType: "application/json",
                responseSchema: stockPredictionSchema,
            },
        });

        const jsonString = response.text;
        const parsedResponse = JSON.parse(jsonString);

        const predictions: Record<string, StockPrediction> = {};
        if (parsedResponse.predictions && Array.isArray(parsedResponse.predictions)) {
            parsedResponse.predictions.forEach((p: any) => {
                predictions[p.symbol] = {
                    predicted_high: p.predicted_high,
                    predicted_low: p.predicted_low,
                    confidence: p.confidence,
                    sentiment: p.sentiment,
                    analysis: p.analysis,
                };
            });
        }
        return predictions;

    } catch (error) {
        console.error("Error fetching stock predictions from Gemini API:", error);
        throw new Error("Failed to fetch AI predictions. Please check your API key and try again.");
    }
    */
};
