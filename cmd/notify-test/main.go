package main

import (
	"fmt"
	"os"

	"github.com/enork/alpaca-trader/internal/config"
	"github.com/enork/alpaca-trader/internal/notify"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	n, err := notify.New(cfg.Notify)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	err = n.SendCashGuardAlert(notify.CashGuardAlert{
		SkippedTickers:   []string{"PLUG", "IBIT"},
		Cash:             511.94,
		ExistingExposure: 300.00,
		AdditionalTotal:  88.06,
		AdditionalPerPut: 44.03,
	})
	if err != nil {
		fmt.Println("FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("email sent")
}
