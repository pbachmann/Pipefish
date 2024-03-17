package vm

import (
	"fmt"
	"pipefish/source/object"
	"pipefish/source/text"
	"pipefish/source/token"
	"pipefish/source/values"

	"strconv"
	"strings"

	"src.elv.sh/pkg/persistent/vector"
)

const (
	SHOW_RUN = true
	DUMMY    = 4294967295
)

type Vm struct {
	// Temporary state: things we change at runtime.
	Mem       []values.Value
	callstack []uint32
	Code      []*Operation

	// Permanent state: things established at compile time.

	StructResolve   StructResolver
	Ub_enums        values.ValueType
	TypeNames       []string
	StructLabels    [][]int    // Array from a struct to its label numbers.
	Enums           [][]string // Array from the number of the enum to a list of the strings of its elements.
	Labels          []string   // Array from the number of a field label to its name.
	Tokens          []*token.Token
	LambdaFactories []*LambdaFactory
}

// All the information we need to make a lambda at a particular point in the code.
type LambdaFactory struct {
	Model  *Lambda  // Copy this to make the lambda.
	ExtMem []uint32 // Then these are the location of the values we're closing over, so we copy them into the lambda.
	Size   uint32   // The size of the memory for a new VM.
}

type Lambda struct {
	Mc        *Vm
	ExtTop    uint32
	PrmTop    uint32
	Dest      uint32
	LocToCall uint32
	Captures  []values.Value
}

type StructResolver interface {
	Add(structNumber int, labels []int) StructResolver // Not the struct type, but its number, i.e. we start at 0.
	Resolve(structNumber int, labelNumber int) int
}

type MapResolver []map[int]int

func (mr MapResolver) Add(structNumber int, labels []int) StructResolver {
	if structNumber != len(mr) {
		panic("That wasn't meant to happen.")
	}
	newMap := make(map[int]int, len(labels))
	for k, v := range labels {
		newMap[v] = k
	}
	mr = append(mr, newMap)
	return mr
}

func (mr MapResolver) Resolve(structNumber int, labelNumber int) int {
	fieldNo, ok := mr[structNumber][labelNumber]
	if ok {
		return fieldNo
	}
	return -1
}

func (vm *Vm) MemTop() uint32 {
	return uint32(len(vm.Mem))
}

func (vm *Vm) That() uint32 {
	return uint32(len(vm.Mem) - 1)
}

func (vm *Vm) ThatToken() uint32 {
	return uint32(len(vm.Tokens) - 1)
}

func (vm *Vm) CodeTop() uint32 {
	return uint32(len(vm.Code))
}

func (vm *Vm) TokenTop() uint32 {
	return uint32(len(vm.Tokens))
}

func (vm *Vm) LfTop() uint32 {
	return uint32(len(vm.LambdaFactories))
}

func (vm *Vm) Next() uint32 {
	return uint32(len(vm.Code))
}

// This adds together two vms. They are presumed to share everything but the code field, the second having been
// generated by the .tempVm method, and so all that is necessary is to add the code, tokens, and lfcs of one to the other while
// changing all the locations to match.
func (vm *Vm) add(vmToAdd *Vm) {
	start := vm.CodeTop()
	tokStart := vm.TokenTop()
	lfStart := vm.LfTop()
	for _, v := range vmToAdd.Code {
		if len(v.Args) > 1 && OPERANDS[v.Opcode].or[len(v.Args)-1] == loc {
			v.Args[len(v.Args)-1] += start
		}
		if len(v.Args) > 1 && OPERANDS[v.Opcode].or[len(v.Args)-1] == tok {
			v.Args[len(v.Args)-1] += tokStart
		}
		if len(v.Args) > 1 && OPERANDS[v.Opcode].or[len(v.Args)-1] == lfc {
			v.Args[len(v.Args)-1] += lfStart
		}
		vm.Code = append(vm.Code, v)
	}
	vm.Tokens = append(vm.Tokens, vmToAdd.Tokens...)
	vm.LambdaFactories = append(vm.LambdaFactories, vmToAdd.LambdaFactories...)
}

var OPCODE_LIST []func(vm *Vm, args []uint32)

var CONSTANTS = []values.Value{values.FALSE, values.TRUE, values.U_OBJ, values.ONE}

func BlankVm() *Vm {
	newVm := &Vm{Mem: CONSTANTS, Ub_enums: values.LB_ENUMS, StructResolve: MapResolver{}}
	// Cross-reference with consts in values.go. TODO --- find something less stupidly brittle to do instead.
	newVm.TypeNames = []string{"UNDEFINED VALUE!!!", "INT_ARRAY", "thunk", "created local constant", "tuple", "error", "unsat", "ref", "null",
		"int", "bool", "string", "float64", "type", "func", "pair", "list", "map", "set", "label"}
	return newVm
}

func (vm *Vm) Run(loc uint32) {
	if SHOW_RUN {
		println()
	}
loop:
	for {
		if SHOW_RUN {
			println(text.GREEN + "    " + vm.DescribeCode(loc) + text.RESET)
		}
		args := vm.Code[loc].Args
		switch vm.Code[loc].Opcode {
		case Addf:
			vm.Mem[args[0]] = values.Value{values.FLOAT, vm.Mem[args[1]].V.(float64) + vm.Mem[args[2]].V.(float64)}
		case Addi:
			vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(int) + vm.Mem[args[2]].V.(int)}
		case Adds:
			vm.Mem[args[0]] = values.Value{values.STRING, vm.Mem[args[1]].V.(string) + vm.Mem[args[2]].V.(string)}
		case Adtk:
			vm.Mem[args[0]] = vm.Mem[args[1]]
			vm.Mem[args[0]].V.(*object.Error).AddToTrace(vm.Tokens[args[2]])
		case Andb:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(bool) && vm.Mem[args[2]].V.(bool)}
		case Asgm:
			vm.Mem[args[0]] = vm.Mem[args[1]]
		case Call:
			offset := args[1]
			for i := args[1]; i < args[2]; i++ {
				vm.Mem[i] = vm.Mem[args[3+i-offset]]
			}
			vm.callstack = append(vm.callstack, loc)
			loc = args[0]
			continue
		case CalT:
			offset := int(args[1]) - 3
			var tupleTime bool
			var tplpt int
			tupleList := vm.Mem[args[2]].V.([]uint32) // This is the hireg of the parameters, and (numbering being exclusive) is the reg containing the integer array saying where tuple captures start.
			for j := 3; j < len(args); j++ {
				if tplpt <= len(tupleList) && j-3 == int(tupleList[tplpt]) {
					tupleTime = true
					vm.Mem[args[1]+tupleList[tplpt]] = values.Value{values.TUPLE, make([]values.Value, 0, 10)}
				}
				// if vm.Mem[i].T == values.BLING {}
				if tupleTime {
					tupleVal := vm.Mem[args[1]+tupleList[tplpt]].V.([]values.Value)
					tupleVal = append(tupleVal, vm.Mem[args[j]])
					vm.Mem[args[1]+tupleList[tplpt]].V = tupleVal
				} else {
					vm.Mem[j+offset] = vm.Mem[args[j]]
				}
			}
			vm.callstack = append(vm.callstack, loc)
			loc = args[0]
			continue
		case Cc11:
			vm.Mem[args[0]] = values.Value{values.TUPLE, []values.Value{vm.Mem[args[1]], vm.Mem[args[2]]}}
		case Cc1T:
			vm.Mem[args[0]] = values.Value{values.TUPLE, append([]values.Value{vm.Mem[args[1]]}, vm.Mem[args[2]].V.([]values.Value)...)}
		case CcT1:
			vm.Mem[args[0]] = values.Value{values.TUPLE, append(vm.Mem[args[1]].V.([]values.Value), vm.Mem[args[2]])}
		case CcTT:
			vm.Mem[args[0]] = values.Value{values.TUPLE, append(vm.Mem[args[1]].V.([]values.Value), vm.Mem[args[2]])}
		case Ccxx:
			if vm.Mem[args[1]].T == values.TUPLE {
				if vm.Mem[args[2]].T == values.TUPLE {
					vm.Mem[args[0]] = values.Value{values.TUPLE, append(vm.Mem[args[1]].V.([]values.Value), vm.Mem[args[2]])}
				} else {
					vm.Mem[args[0]] = values.Value{values.TUPLE, append(vm.Mem[args[1]].V.([]values.Value), vm.Mem[args[2]])}
				}
			} else {
				if vm.Mem[args[2]].T == values.TUPLE {
					vm.Mem[args[0]] = values.Value{values.TUPLE, append([]values.Value{vm.Mem[args[1]]}, vm.Mem[args[2]].V.([]values.Value)...)}
				} else {
					vm.Mem[args[0]] = values.Value{values.TUPLE, []values.Value{vm.Mem[args[1]], vm.Mem[args[2]]}}
				}
			}
		case Cv1T:
			vm.Mem[args[0]] = values.Value{values.TUPLE, []values.Value{vm.Mem[args[1]]}}
		case CvTT:
			slice := make([]values.Value, len(args)-1)
			for i := 0; i < len(slice); i++ {
				slice[i] = vm.Mem[args[i+1]]
			}
			vm.Mem[args[0]] = values.Value{values.TUPLE, slice}
		case Divf:
			vm.Mem[args[0]] = values.Value{values.FLOAT, vm.Mem[args[1]].V.(float64) / vm.Mem[args[2]].V.(float64)}
		case Divi:
			vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(int) / vm.Mem[args[2]].V.(int)}
		case Dofn:
			lhs := vm.Mem[args[1]].V.(Lambda)
			for i := 0; i < int(lhs.PrmTop-lhs.ExtTop); i++ {
				lhs.Mc.Mem[int(lhs.ExtTop)+i] = vm.Mem[args[2+i]]
			}
			copy(lhs.Captures, vm.Mem)
			lhs.Mc.Run(lhs.LocToCall)
			vm.Mem[args[0]] = lhs.Mc.Mem[lhs.Dest]
		case Dref:
			vm.Mem[args[0]] = vm.Mem[vm.Mem[args[1]].V.(uint32)]
		case Equb:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(bool) == vm.Mem[args[2]].V.(bool)}
		case Equf:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(float64) == vm.Mem[args[2]].V.(float64)}
		case Equi:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(int) == vm.Mem[args[2]].V.(int)}
		case Equs:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(string) == vm.Mem[args[2]].V.(string)}
		case Flti:
			vm.Mem[args[0]] = values.Value{values.FLOAT, float64(vm.Mem[args[1]].V.(int))}
		case Flts:
			i, err := strconv.ParseFloat(vm.Mem[args[1]].V.(string), 64)
			if err != nil {
				vm.Mem[args[0]] = values.Value{values.ERROR, DUMMY}
			} else {
				vm.Mem[args[0]] = values.Value{values.FLOAT, i}
			}
		case Gtef:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(float64) >= vm.Mem[args[2]].V.(float64)}
		case Gtei:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(int) >= vm.Mem[args[2]].V.(int)}
		case Gthf:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(float64) > vm.Mem[args[2]].V.(float64)}
		case Gthi:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(int) > vm.Mem[args[2]].V.(int)}
		case Halt:
			break loop
		case Idfn:
			vm.Mem[args[0]] = vm.Mem[args[1]]
		case Intf:
			vm.Mem[args[0]] = values.Value{values.INT, int(vm.Mem[args[1]].V.(float64))}
		case Ints:
			i, err := strconv.Atoi(vm.Mem[args[1]].V.(string))
			if err != nil {
				vm.Mem[args[0]] = values.Value{values.ERROR, DUMMY}
			} else {
				vm.Mem[args[0]] = values.Value{values.INT, i}
			}
		case IdxL:
			vec := vm.Mem[args[1]].V.(vector.Vector)
			ix := vm.Mem[args[2]].V.(int)
			val, ok := vec.Index(ix)
			if !ok {
				vm.Mem[args[0]] = vm.Mem[args[3]]

			} else {
				vm.Mem[args[0]] = val.(values.Value)
			}
		case Idxp:
			pair := vm.Mem[args[1]].V.([]values.Value)
			ix := vm.Mem[args[2]].V.(int)
			ok := ix == 0 || ix == 1
			if ok {
				vm.Mem[args[0]] = pair[ix]
			} else {
				vm.Mem[args[0]] = vm.Mem[args[3]]
			}
		case Idxs:
			str := vm.Mem[args[1]].V.(string)
			ix := vm.Mem[args[2]].V.(int)
			ok := 0 <= ix && ix < len(str)
			if ok {
				val := values.Value{values.STRING, string(str[ix])}
				vm.Mem[args[0]] = val
			} else {
				vm.Mem[args[0]] = vm.Mem[args[3]]
			}
		case Idxt:
			typ := vm.Mem[args[1]].V.(values.ValueType)
			if typ < values.LB_ENUMS || vm.Ub_enums <= typ {
				vm.Mem[args[0]] = vm.Mem[args[3]]
				break
			}
			ix := vm.Mem[args[2]].V.(int)
			ok := 0 <= ix && ix < len(vm.Enums[typ-values.LB_ENUMS])
			if ok {
				vm.Mem[args[0]] = values.Value{typ, ix}
			} else {
				vm.Mem[args[0]] = vm.Mem[args[4]]
			}
		case IdxT:
			tuple := vm.Mem[args[1]].V.([]values.Value)
			ix := vm.Mem[args[2]].V.(int)
			ok := 0 <= ix && ix < len(tuple)
			if ok {
				vm.Mem[args[0]] = tuple[ix]
			} else {
				vm.Mem[args[0]] = vm.Mem[args[3]]
			}
		case IxTn:
			vm.Mem[args[0]] = vm.Mem[args[1]].V.([]values.Value)[args[2]]
		case IxZl:
			ix := vm.StructResolve.Resolve(int(vm.Mem[args[1]].T-vm.Ub_enums), vm.Mem[args[2]].V.(int))
			vm.Mem[args[0]] = vm.Mem[args[1]].V.([]values.Value)[ix]
		case IxZn:
			vm.Mem[args[0]] = vm.Mem[args[1]].V.([]values.Value)[args[2]]
		case Jmp:
			loc = args[0]
			continue
		case Jsr:
			vm.callstack = append(vm.callstack, loc)
			loc = args[0]
			continue
		case KeyM:
			vm.Mem[args[0]] = values.Value{values.LIST, vm.Mem[args[1]].V.(*values.Map).AsVector()}
		case KeyZ:
			result := vector.Empty
			for _, labelNumber := range vm.StructLabels[vm.Mem[args[1]].T-vm.Ub_enums] {
				result = result.Conj(values.Value{values.LABEL, labelNumber})
			}
			vm.Mem[args[0]] = values.Value{values.LIST, result}
		case LenL:
			vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(vector.Vector).Len()}
		case LenM:
			vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(*values.Map).Len()}
		case Lens:
			vm.Mem[args[0]] = values.Value{values.INT, len(vm.Mem[args[1]].V.(string))}
		case LenS:
			vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(values.Set).Len()}
		case LenT:
			vm.Mem[args[0]] = values.Value{values.INT, len(vm.Mem[args[1]].V.([]values.Value))}
		case List:
			list := vector.Empty
			if vm.Mem[args[1]].T == values.TUPLE {
				for _, v := range vm.Mem[args[1]].V.([]values.Value) {
					list = list.Conj(v)
				}
			} else {
				list = list.Conj(vm.Mem[args[1]])
			}
			vm.Mem[args[0]] = values.Value{values.LIST, list}
		case Litx:
			vm.Mem[args[0]] = values.Value{values.STRING, vm.Literal(vm.Mem[args[1]])}
		case Mker:
			vm.Mem[args[0]] = values.Value{values.ERROR, &object.Error{ErrorId: "eval/user", Message: vm.Mem[args[1]].V.(string), Token: vm.Tokens[args[2]]}}
		case Mkfn:
			lf := vm.LambdaFactories[args[1]]
			newLambda := *lf.Model
			newLambda.Captures = make([]values.Value, len(lf.ExtMem))
			for i, v := range lf.ExtMem {
				newLambda.Captures[i] = vm.Mem[v]
			}
			vm.Mem[args[0]] = values.Value{values.FUNC, newLambda}
		case Mkpr:
			vm.Mem[args[0]] = values.Value{values.PAIR, []values.Value{vm.Mem[args[1]], vm.Mem[args[2]]}}
		case Mkst:
			result := values.Set{}
			for _, v := range vm.Mem[args[1]].V.([]values.Value) {
				if !((values.NULL <= v.T && v.T < values.PAIR) || (values.LB_ENUMS <= v.T && v.T < vm.Ub_enums)) {
					vm.Mem[args[0]] = vm.Mem[vm.That()] // I.e. an error created before the mkst call.
				}
				result = result.Add(v)
			}
			vm.Mem[args[0]] = values.Value{values.SET, result}
		case Mkmp:
			result := &values.Map{}
			for _, p := range vm.Mem[args[1]].V.([]values.Value) {
				if p.T != values.PAIR {
					vm.Mem[args[0]] = vm.Mem[vm.That()-1] // I.e. an error created before the mkmp call.
					break
				}
				k := p.V.([]values.Value)[0]
				v := p.V.([]values.Value)[1]
				if !((values.NULL <= v.T && v.T < values.PAIR) || (values.LB_ENUMS <= v.T && v.T < vm.Ub_enums)) {
					vm.Mem[args[0]] = vm.Mem[vm.That()] // I.e. an error created before the mkst call.
				}
				result.Set(k, v)
			}
			vm.Mem[args[0]] = values.Value{values.MAP, result}
		case Modi:
			vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(int) % vm.Mem[args[2]].V.(int)}
		case Mulf:
			vm.Mem[args[0]] = values.Value{values.FLOAT, vm.Mem[args[1]].V.(float64) * vm.Mem[args[2]].V.(float64)}
		case Muli:
			vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(int) * vm.Mem[args[2]].V.(int)}
		case Negf:
			vm.Mem[args[0]] = values.Value{values.FLOAT, -vm.Mem[args[1]].V.(float64)}
		case Negi:
			vm.Mem[args[0]] = values.Value{values.INT, -vm.Mem[args[1]].V.(int)}
		case Notb:
			vm.Mem[args[0]] = values.Value{values.BOOL, !vm.Mem[args[1]].V.(bool)}
		case Orb:
			vm.Mem[args[0]] = values.Value{values.BOOL, (vm.Mem[args[1]].V.(bool) || vm.Mem[args[2]].V.(bool))}
		case QlnT:
			if len(vm.Mem[args[0]].V.([]values.Value)) == int(args[1]) {
				loc = loc + 1
			} else {
				loc = args[2]
			}
		case Qsng:
			if vm.Mem[args[0]].T >= values.INT {
				loc = loc + 1
			} else {
				loc = args[1]
			}
			continue
		case QsnQ:
			if vm.Mem[args[0]].T >= values.NULL {
				loc = loc + 1
			} else {
				loc = args[1]
			}
			continue
		case Qtru:
			if vm.Mem[args[0]].V.(bool) {
				loc = loc + 1
			} else {
				loc = args[1]
			}
			continue
		case Qtyp:
			if vm.Mem[args[0]].T == values.ValueType(args[1]) {
				loc = loc + 1
			} else {
				loc = args[2]
			}
			continue
		case Ret:
			if len(vm.callstack) == 0 {
				break loop
			}
			loc = vm.callstack[len(vm.callstack)-1]
			vm.callstack = vm.callstack[0 : len(vm.callstack)-1]
		case Strc:
			fields := make([]values.Value, 0, len(args)-2)
			for _, loc := range args[2:] {
				fields = append(fields, vm.Mem[loc])
			}
			vm.Mem[args[0]] = values.Value{values.ValueType(args[1]), fields}
		case Strx:
			vm.Mem[args[0]] = values.Value{values.STRING, vm.describe(vm.Mem[args[1]])}
		case Subf:
			vm.Mem[args[0]] = values.Value{values.FLOAT, vm.Mem[args[1]].V.(float64) - vm.Mem[args[2]].V.(float64)}
		case Subi:
			vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(int) - vm.Mem[args[2]].V.(int)}
		case Thnk:
			vm.Mem[args[0]].T = values.THUNK
			vm.Mem[args[0]].V = args[1]
		case TupL:
			vector := vm.Mem[args[1]].V.(vector.Vector)
			length := vector.Len()
			slice := make([]values.Value, length)
			for i := 0; i < length; i++ {
				element, _ := vector.Index(i)
				slice[i] = element.(values.Value)
			}
			vm.Mem[args[0]] = values.Value{values.TUPLE, slice}
		case Typx:
			vm.Mem[args[0]] = values.Value{values.TYPE, vm.Mem[args[1]].T}
		case Untk:
			if (vm.Mem[args[0]].T) == values.THUNK {
				vm.callstack = append(vm.callstack, loc)
				loc = vm.Mem[args[0]].V.(uint32)
				continue
			}
		default:
			panic("Unhandled opcode!")
		}
		loc++
	}
	if SHOW_RUN {
		println()
	}
}

func (vm *Vm) DescribeCode(loc uint32) string {
	prefix := "@" + strconv.Itoa(int(loc)) + " : "
	spaces := strings.Repeat(" ", 6-len(prefix))
	return spaces + prefix + describe(vm.Code[loc])
}

func (vm *Vm) describeType(t values.ValueType) string {
	return vm.TypeNames[t]
}

func (vm *Vm) describe(v values.Value) string {
	if v.T >= vm.Ub_enums { // We have a struct.
		var buf strings.Builder
		buf.WriteString(vm.TypeNames[v.T])
		buf.WriteString(" with (")
		var sep string
		labels := vm.StructLabels[v.T-vm.Ub_enums]
		for i, el := range v.V.([]values.Value) {
			fmt.Fprintf(&buf, "%s%s::%s", sep, vm.Labels[labels[i]], vm.describe(el))
			sep = ", "
		}
		buf.WriteByte(')')
		return buf.String()
	}
	if values.LB_ENUMS <= v.T && v.T < values.ValueType(vm.Ub_enums) { // We have an enum.
		return vm.Enums[v.T-values.LB_ENUMS][v.V.(int)]
	}
	switch v.T {
	case values.BOOL:
		if v.V.(bool) {
			return "true"
		} else {
			return "false"
		}
	case values.ERROR:
		ob := v.V.(*object.Error)
		if ob.ErrorId != "eval/user" {
			ob = object.CreateErr(ob.ErrorId, ob.Token, ob.Args...)
		}
		return text.Pretty(text.RT_ERROR+ob.Message+text.DescribePos(ob.Token)+".", 0, 80)
	case values.FLOAT:
		return strconv.FormatFloat(v.V.(float64), 'f', 8, 64)
	case values.FUNC:
		return "lambda function"
	case values.INT:
		return strconv.Itoa(v.V.(int))
	case values.INT_ARRAY:
		var buf strings.Builder
		buf.WriteString("INT_ARRAY(")
		var sep string
		for _, v := range v.V.([]uint32) {
			fmt.Fprintf(&buf, "%s%v", sep, v)
			sep = ", "
		}
		buf.WriteByte(')')
		return buf.String()
	case values.LABEL:
		return vm.Labels[v.V.(int)]
	case values.LIST:
		var buf strings.Builder
		buf.WriteString("[")
		var sep string
		for i := 0; i < v.V.(vector.Vector).Len(); i++ {
			el, _ := v.V.(vector.Vector).Index(i)
			fmt.Fprintf(&buf, "%s%s", sep, vm.describe(el.(values.Value)))
			sep = ", "
		}
		buf.WriteByte(']')
		return buf.String()
	case values.MAP:
		var buf strings.Builder
		buf.WriteString("map(")
		var sep string
		(v.V.(*values.Map)).Range(func(k, v values.Value) {
			fmt.Fprintf(&buf, "%s%v::%v", sep, vm.describe(k), vm.describe(v))
			sep = ", "
		})
		buf.WriteByte(')')
		return buf.String()
	case values.NULL:
		return "NULL"
	case values.PAIR:
		vals := v.V.([]values.Value)
		return vm.describe(vals[0]) + "::" + vm.describe(vals[1])
	case values.SET:
		var buf strings.Builder
		buf.WriteString("set(")
		var sep string
		v.V.(values.Set).Range(func(k values.Value) {
			fmt.Fprintf(&buf, "%s%s", sep, vm.describe(k))
			sep = ", "
		})
		buf.WriteByte(')')
		return buf.String()
	case values.STRING:
		return v.V.(string)
	case values.THUNK:
		return "thunk"
	case values.TUPLE:
		result := make([]string, len(v.V.([]values.Value)))
		for i, v := range v.V.([]values.Value) {
			result[i] = vm.describe(v)
		}
		prefix := "("
		if len(result) == 1 {
			prefix = "tuple("
		}
		return prefix + strings.Join(result, ", ") + ")"
	case values.TYPE:
		return vm.describeType(v.V.(values.ValueType))
	case values.UNDEFINED_VALUE:
		return "UNDEFINED VALUE!!!"
	case values.UNSAT:
		return "UNSATIFIED CONDITIONAL!!!"
	}
	println("Undescribable value", v.T)
	panic("can't describe value")
}

func (vm *Vm) Literal(v values.Value) string {
	switch v.T {
	case values.STRING:
		return "\"" + v.V.(string) + "\""
	default:
		return vm.describe(v)
	}
}
