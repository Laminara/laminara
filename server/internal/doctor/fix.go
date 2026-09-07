package doctor

import (
	"context"
	"fmt"
	"io"

	"github.com/laminara/laminara/server/internal/diag"
)

func Fix(ctx context.Context, out io.Writer, results []diag.Result) int {
	fixed := 0
	for _, result := range order(results) {
		if result.Remedy.Apply == nil {
			continue
		}
		if err := result.Remedy.Apply(ctx); err != nil {
			fmt.Fprintf(out, "  не вышло  %-24s %v\n", result.What, err)
			continue
		}
		fixed++
		fmt.Fprintf(out, "  починено  %-24s %s\n", result.What, result.Detail)
	}
	if fixed == 0 {
		fmt.Fprintln(out, "Само здесь ничего не чинится — остальное правится командами выше.")
		return 0
	}
	fmt.Fprintf(out, "\nПочинено: %d. Проверьте ещё раз: laminara-server doctor\n", fixed)
	return fixed
}
