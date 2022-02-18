package guac

type Filter interface {
	Filter(*Instruction) (*Instruction, error)
}
