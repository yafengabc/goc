module goc

go 1.21

require (
	goc/common v0.0.0
	goc/frontend v0.0.0
	goa v0.0.0
)

replace goc/common => ../common

replace goc/frontend => ../frontend

replace goa => ../goa
