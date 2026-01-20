package data

import (
	"fmt"
	"math"
	"strings"

	"stock-ai/backend/models"
)

type indicatorSnapshot struct {
	Close       float64
	ChangePct   float64
	Volume      int64
	MA5         float64
	MA10        float64
	MA20        float64
	RSI         float64
	MACDDif     float64
	MACDDea     float64
	MACDHist    float64
	K           float64
	D           float64
	J           float64
	BR          float64
	AR          float64
	PlusDI      float64
	MinusDI     float64
	ADX         float64
	CR          float64
	PSY         float64
	PSYMA       float64
	DMA         float64
	AMA         float64
	TRIX        float64
	MATRIX      float64
	RangeHigh30 float64
	RangeLow30  float64
}

func summarizeKLinePeriod(title string, items []models.KLineData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s\n", title))
	if len(items) == 0 {
		sb.WriteString("- 暂无数据\n\n")
		return sb.String()
	}

	snapshot := buildIndicatorSnapshot(items)
	sb.WriteString(fmt.Sprintf("- 最新收盘：%.2f（较上一周期 %.2f%%），成交量：%s\n", snapshot.Close, snapshot.ChangePct, formatVolume(snapshot.Volume)))
	sb.WriteString(fmt.Sprintf("- 均线：MA5 %.2f / MA10 %.2f / MA20 %.2f\n", snapshot.MA5, snapshot.MA10, snapshot.MA20))
	sb.WriteString(fmt.Sprintf("- RSI14：%.2f\n", snapshot.RSI))
	sb.WriteString(fmt.Sprintf("- MACD：DIF %.3f，DEA %.3f，柱值 %.3f\n", snapshot.MACDDif, snapshot.MACDDea, snapshot.MACDHist))
	sb.WriteString(fmt.Sprintf("- KDJ(LDJ)：K %.2f，D %.2f，J %.2f\n", snapshot.K, snapshot.D, snapshot.J))
	sb.WriteString(fmt.Sprintf("- KD指标：K %.2f / D %.2f\n", snapshot.K, snapshot.D))
	sb.WriteString(fmt.Sprintf("- BRAR：BR %.2f，AR %.2f\n", snapshot.BR, snapshot.AR))
	sb.WriteString(fmt.Sprintf("- DMI：+DI %.2f / -DI %.2f，ADX %.2f\n", snapshot.PlusDI, snapshot.MinusDI, snapshot.ADX))
	sb.WriteString(fmt.Sprintf("- CR：%.2f\n", snapshot.CR))
	sb.WriteString(fmt.Sprintf("- PSY/PSYMA：%.2f / %.2f\n", snapshot.PSY, snapshot.PSYMA))
	sb.WriteString(fmt.Sprintf("- DMA/AMA：%.2f / %.2f\n", snapshot.DMA, snapshot.AMA))
	sb.WriteString(fmt.Sprintf("- TRIX/MATRIX：%.2f / %.2f\n", snapshot.TRIX, snapshot.MATRIX))
	sb.WriteString(fmt.Sprintf("- 近30周期波动区间：%.2f ~ %.2f\n\n", snapshot.RangeLow30, snapshot.RangeHigh30))
	return sb.String()
}

func buildIndicatorSnapshot(items []models.KLineData) indicatorSnapshot {
	length := len(items)
	closes := make([]float64, length)
	for i, k := range items {
		closes[i] = k.Close
	}

	latest := items[length-1]
	prevClose := latest.Open
	if length > 1 {
		prevClose = items[length-2].Close
	}
	changePct := 0.0
	if prevClose != 0 {
		changePct = (latest.Close - prevClose) / prevClose * 100
	}

	ma5 := calcSimpleMA(closes, 5)
	ma10 := calcSimpleMA(closes, 10)
	ma20 := calcSimpleMA(closes, 20)
	rsi := calcRSI(closes, 14)
	dif, dea, hist := calcMACD(closes)
	k, d, j := calcKDJ(items, 9, 3, 3)
	br, ar := calcBRARValue(items, 26)
	plusDI, minusDI, adx := calcDMIValue(items, 14)
	cr := calcCRValue(items, 26)
	psy, psyma := calcPSYValue(closes, 12, 6)
	dma, ama := calcDMAValue(closes, 10, 50, 10)
	trix, matrix := calcTRIXValue(closes, 12, 9)
	low, high := calcRange(items, 30)

	return indicatorSnapshot{
		Close:       latest.Close,
		ChangePct:   changePct,
		Volume:      latest.Volume,
		MA5:         ma5,
		MA10:        ma10,
		MA20:        ma20,
		RSI:         rsi,
		MACDDif:     dif,
		MACDDea:     dea,
		MACDHist:    hist,
		K:           k,
		D:           d,
		J:           j,
		BR:          br,
		AR:          ar,
		PlusDI:      plusDI,
		MinusDI:     minusDI,
		ADX:         adx,
		CR:          cr,
		PSY:         psy,
		PSYMA:       psyma,
		DMA:         dma,
		AMA:         ama,
		TRIX:        trix,
		MATRIX:      matrix,
		RangeHigh30: high,
		RangeLow30:  low,
	}
}

func calcSimpleMA(values []float64, period int) float64 {
	if period <= 0 || len(values) < period {
		return 0
	}
	sum := 0.0
	for i := len(values) - period; i < len(values); i++ {
		sum += values[i]
	}
	return sum / float64(period)
}

func calcEMA(values []float64, period int) []float64 {
	result := make([]float64, len(values))
	if period <= 0 || len(values) == 0 {
		return result
	}
	k := 2 / float64(period+1)
	ema := values[0]
	result[0] = ema
	for i := 1; i < len(values); i++ {
		ema = values[i]*k + ema*(1-k)
		result[i] = ema
	}
	return result
}

func calcMACD(values []float64) (float64, float64, float64) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	shortEMA := calcEMA(values, 12)
	longEMA := calcEMA(values, 26)
	difArr := make([]float64, len(values))
	for i := range values {
		difArr[i] = shortEMA[i] - longEMA[i]
	}
	deaArr := calcEMA(difArr, 9)
	last := len(values) - 1
	dif := difArr[last]
	dea := deaArr[last]
	hist := (dif - dea) * 2
	return round(dif, 3), round(dea, 3), round(hist, 3)
}

func calcKDJ(items []models.KLineData, period, kPeriod, dPeriod int) (float64, float64, float64) {
	if len(items) == 0 {
		return 0, 0, 0
	}
	k := 50.0
	d := 50.0
	for i := 0; i < len(items); i++ {
		start := i - period + 1
		if start < 0 {
			start = 0
		}
		highest := math.Inf(-1)
		lowest := math.Inf(1)
		for j := start; j <= i; j++ {
			if items[j].High > highest {
				highest = items[j].High
			}
			if items[j].Low < lowest {
				lowest = items[j].Low
			}
		}
		rangeVal := highest - lowest
		rsv := 50.0
		if rangeVal != 0 && !math.IsInf(highest, -1) && !math.IsInf(lowest, 1) {
			rsv = (items[i].Close - lowest) / rangeVal * 100
		}
		k = ((float64(kPeriod-1))*k + rsv) / float64(kPeriod)
		d = ((float64(dPeriod-1))*d + k) / float64(dPeriod)
	}
	j := 3*k - 2*d
	return round(k, 2), round(d, 2), round(j, 2)
}

func calcRange(items []models.KLineData, count int) (float64, float64) {
	if len(items) == 0 {
		return 0, 0
	}
	start := 0
	if len(items) > count {
		start = len(items) - count
	}
	low := math.Inf(1)
	high := math.Inf(-1)
	for i := start; i < len(items); i++ {
		if items[i].Low < low {
			low = items[i].Low
		}
		if items[i].High > high {
			high = items[i].High
		}
	}
	if math.IsInf(low, 1) {
		low = 0
	}
	if math.IsInf(high, -1) {
		high = 0
	}
	return round(low, 2), round(high, 2)
}

func round(val float64, precision int) float64 {
	if precision <= 0 {
		return math.Round(val)
	}
	pow := math.Pow10(precision)
	return math.Round(val*pow) / pow
}

func calcRSI(values []float64, period int) float64 {
	if len(values) <= period {
		return 0
	}
	avgGain := 0.0
	avgLoss := 0.0
	for i := 1; i <= period; i++ {
		change := values[i] - values[i-1]
		if change > 0 {
			avgGain += change
		} else {
			avgLoss += -change
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)
	for i := period + 1; i < len(values); i++ {
		change := values[i] - values[i-1]
		gain := math.Max(change, 0)
		loss := math.Max(-change, 0)
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
	}
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return round(100-(100/(1+rs)), 2)
}

func calcBRARValue(items []models.KLineData, period int) (float64, float64) {
	if len(items) < period {
		return 0, 0
	}
	sumHClose := 0.0
	sumCloseL := 0.0
	sumHighOpen := 0.0
	sumOpenLow := 0.0
	start := len(items) - period
	for i := start; i < len(items); i++ {
		cur := items[i]
		prevClose := cur.Close
		if i > 0 {
			prevClose = items[i-1].Close
		}
		sumHClose += math.Max(cur.High-prevClose, 0)
		sumCloseL += math.Max(prevClose-cur.Low, 0)
		sumHighOpen += cur.High - cur.Open
		sumOpenLow += cur.Open - cur.Low
	}
	br := 0.0
	if sumCloseL != 0 {
		br = round(sumHClose/sumCloseL*100, 2)
	}
	ar := 0.0
	if sumOpenLow != 0 {
		ar = round(sumHighOpen/sumOpenLow*100, 2)
	}
	return br, ar
}

func calcDMIValue(items []models.KLineData, period int) (float64, float64, float64) {
	if len(items) < period+1 {
		return 0, 0, 0
	}
	var trList, plusList, minusList []float64
	var plusDI, minusDI, adx float64
	var dxValues []float64
	for i := 1; i < len(items); i++ {
		cur := items[i]
		prev := items[i-1]
		upMove := cur.High - prev.High
		downMove := prev.Low - cur.Low
		plusDM := 0.0
		minusDM := 0.0
		if upMove > downMove && upMove > 0 {
			plusDM = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDM = downMove
		}
		tr := math.Max(cur.High-cur.Low, math.Max(math.Abs(cur.High-prev.Close), math.Abs(cur.Low-prev.Close)))
		trList = append(trList, tr)
		plusList = append(plusList, plusDM)
		minusList = append(minusList, minusDM)
		if len(trList) > period {
			trList = trList[1:]
			plusList = plusList[1:]
			minusList = minusList[1:]
		}
		if len(trList) == period {
			sumTR := sumSlice(trList)
			sumPlus := sumSlice(plusList)
			sumMinus := sumSlice(minusList)
			if sumTR != 0 {
				plusDI = round((sumPlus/sumTR)*100, 2)
				minusDI = round((sumMinus/sumTR)*100, 2)
				if plusDI+minusDI != 0 {
					dx := math.Abs(plusDI-minusDI) / (plusDI + minusDI) * 100
					dxValues = append(dxValues, dx)
				}
			}
		}
	}
	if len(dxValues) > 0 {
		if len(dxValues) > period {
			dxValues = dxValues[len(dxValues)-period:]
		}
		adx = round(sumSlice(dxValues)/float64(len(dxValues)), 2)
	}
	return plusDI, minusDI, adx
}

func calcCRValue(items []models.KLineData, period int) float64 {
	if len(items) <= period {
		return 0
	}
	sumUp := 0.0
	sumDown := 0.0
	start := len(items) - period
	for j := start; j < len(items); j++ {
		prev := items[j]
		if j > 0 {
			prev = items[j-1]
		}
		mid := (prev.High + prev.Low + prev.Close) / 3
		sumUp += math.Max(items[j].High-mid, 0)
		sumDown += math.Max(mid-items[j].Low, 0)
	}
	if sumDown == 0 {
		return 0
	}
	return round(sumUp/sumDown*100, 2)
}

func calcPSYValue(values []float64, period, maPeriod int) (float64, float64) {
	if len(values) <= period {
		return 0, 0
	}
	count := 0.0
	for i := len(values) - period; i < len(values); i++ {
		if i > 0 && values[i] > values[i-1] {
			count++
		}
	}
	psy := round((count/float64(period))*100, 2)
	// 简化的PSYMA：对最近maPeriod个psy值再平均
	psyValues := []float64{}
	for i := 0; i < maPeriod && len(values)-period-i >= 0; i++ {
		idx := len(values) - i
		if idx-period <= 0 {
			break
		}
		count := 0.0
		for j := idx - period; j < idx; j++ {
			if j > 0 && values[j] > values[j-1] {
				count++
			}
		}
		psyValues = append(psyValues, (count/float64(period))*100)
	}
	psyma := 0.0
	if len(psyValues) > 0 {
		psyma = round(sumSlice(psyValues)/float64(len(psyValues)), 2)
	}
	return psy, psyma
}

func calcDMAValue(values []float64, shortPeriod, longPeriod, avgPeriod int) (float64, float64) {
	if len(values) < longPeriod {
		return 0, 0
	}
	shortMA := movingAverage(values, shortPeriod)
	longMA := movingAverage(values, longPeriod)
	dmaSeries := make([]float64, len(values))
	for i := 0; i < len(values); i++ {
		dmaSeries[i] = shortMA[i] - longMA[i]
	}
	dma := round(dmaSeries[len(dmaSeries)-1], 3)
	amaSeries := movingAverage(dmaSeries[longPeriod-1:], avgPeriod)
	ama := round(amaSeries[len(amaSeries)-1], 3)
	return dma, ama
}

func calcTRIXValue(values []float64, period, maPeriod int) (float64, float64) {
	if len(values) <= period {
		return 0, 0
	}
	ema1 := emaSeries(values, period)
	ema2 := emaSeries(ema1, period)
	ema3 := emaSeries(ema2, period)
	trixSeries := make([]float64, len(values))
	for i := 1; i < len(values); i++ {
		if ema3[i-1] == 0 {
			trixSeries[i] = 0
			continue
		}
		trixSeries[i] = ((ema3[i] - ema3[i-1]) / ema3[i-1]) * 100
	}
	trix := round(trixSeries[len(trixSeries)-1], 3)
	matrixSeries := movingAverage(trixSeries, maPeriod)
	matrix := round(matrixSeries[len(matrixSeries)-1], 3)
	return trix, matrix
}

func movingAverage(values []float64, period int) []float64 {
	result := make([]float64, len(values))
	if period <= 0 {
		return result
	}
	sum := 0.0
	for i := 0; i < len(values); i++ {
		sum += values[i]
		if i >= period {
			sum -= values[i-period]
			result[i] = sum / float64(period)
		} else {
			result[i] = sum / float64(i+1)
		}
	}
	return result
}

func emaSeries(values []float64, period int) []float64 {
	result := make([]float64, len(values))
	if period <= 0 || len(values) == 0 {
		return result
	}
	k := 2 / float64(period+1)
	result[0] = values[0]
	for i := 1; i < len(values); i++ {
		result[i] = values[i]*k + result[i-1]*(1-k)
	}
	return result
}

func formatVolume(volume int64) string {
	switch {
	case volume >= 100000000:
		return fmt.Sprintf("%.2f亿", float64(volume)/100000000)
	case volume >= 10000:
		return fmt.Sprintf("%.2f万", float64(volume)/10000)
	default:
		return fmt.Sprintf("%d", volume)
	}
}

func sumSlice(values []float64) float64 {
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum
}
