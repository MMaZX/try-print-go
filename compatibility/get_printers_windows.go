//go:build windows

package compatibility

import "github.com/yusufpapurcu/wmi"

type Win32_Printer struct {
    Name string
}

func GetInstalledPrinters() ([]string, error) {
    var printers []Win32_Printer
    q := wmi.CreateQuery(&printers, "")
    err := wmi.Query(q, &printers)
    if err != nil {
        return nil, err
    }

    result := make([]string, 0, len(printers))
    for _, p := range printers {
        result = append(result, p.Name)
    }
    return result, nil
}
