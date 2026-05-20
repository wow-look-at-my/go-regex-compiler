package match

func InRange[T byte | rune](c, lo, hi T) bool { return c >= lo && c <= hi }
