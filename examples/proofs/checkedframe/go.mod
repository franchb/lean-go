module example.com/lean-go-checkedframe-demo

go 1.27.4

require (
	example.com/lean-go-crc16-demo v0.0.0
	example.com/lean-go-wire16-demo v0.0.0
)

replace example.com/lean-go-crc16-demo => ../crc16

replace example.com/lean-go-wire16-demo => ../wire16
