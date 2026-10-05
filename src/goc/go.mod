module goc

go 1.21

require (
	goa v0.0.0
	goc/common v0.0.0
	goc/frontend v0.0.0
)

replace goc/common => ../common

replace goc/frontend => ../frontend

replace goa => ../goa
