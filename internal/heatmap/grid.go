package heatmap

func (c Cell) WorldWidth() float64 {
	return c.MaximumX - c.MinimumX
}

func (c Cell) WorldDepth() float64 {
	return c.MaximumZ - c.MinimumZ
}
