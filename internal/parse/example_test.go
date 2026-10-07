package parse_test

import (
	"fmt"

	"parcial1/internal/parse"
)

func ExampleParsearManual() {
	linea := `190.12.44.3 - alex [15/Mar/2019:14:22:07 -0300] "GET /usr/login HTTP/1.1" 200 4981 "-" "curl/7.81.0" 342`
	e, err := parse.ParsearManual(linea)
	if err != nil {
		panic(err)
	}
	fmt.Println(e.IP, e.Usuario, e.Metodo, e.Ruta, e.Estado, e.Fecha.Hour(), e.RespMs)
	// Output: 190.12.44.3 alex GET /usr/login 200 14 342
}
