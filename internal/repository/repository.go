package repository

type Payment interface {
}

type Outbox interface {
}

type Repositories struct {
	Payment Payment
	Outbox  Outbox
}

func NewRepositories() *Repositories {
	return &Repositories{}
}
