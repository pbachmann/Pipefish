package vm

import (
	"charm/source/set"
	"strconv"
)

const ( // Cross-reference with typeNames in blankVm()
	ERROR simpleType = iota // Some code may depend on the order of early elements.
	NULL
	INT
	BOOL
	STRING
	FLOAT
	UNSAT
	THUNK
	TUPLE
	ARGUMENTS
	CREATED_LOCAL_CONSTANT
	TYPE_ERROR
	COMPILATION_ERROR
	LB_ENUMS // I.e the first of the enums.
)

type Value struct {
	T simpleType
	V any
}

var (
	FALSE = Value{T: BOOL, V: false}
	TRUE  = Value{T: BOOL, V: true}
	U_OBJ = Value{T: UNSAT}
)

const (
	C_FALSE = iota
	C_TRUE
	C_U_OBJ
)

type varAccess int

const (
	GLOBAL_CONSTANT_PUBLIC varAccess = iota
	GLOBAL_VARIABLE_PUBLIC
	FUNCTION_ARGUMENT
	LOCAL_CONSTANT_THUNK
)

type variable struct {
	mLoc   uint32
	access varAccess
	types  alternateType
}

type environment struct {
	data map[string]variable
	ext  *environment
}

func newEnvironment() *environment {
	return &environment{data: make(map[string]variable), ext: nil}
}

func (env *environment) getVar(name string) (*variable, bool) {
	if env == nil {
		return nil, false
	}
	v, ok := env.data[name]
	if ok {
		return &v, true
	}
	return env.ext.getVar(name)
}

type typeScheme interface {
	compare(u typeScheme) int
}

// Finds all the possible lengths of tuples in a typeScheme. (Single values have length 1. Non-finite tuples have length -1.)
// This allows us to figure out if we need to generate a check on the length of a tuple or whether we can take it for granted
// at compile time.
func lengths(t typeScheme) set.Set[int] {
	result := make(set.Set[int])
	switch t := t.(type) {
	case simpleType:
		result.Add(1)
		return result
	case typedTupleType:
		result.Add(-1)
		return result
	case alternateType:
		for _, v := range t {
			newSet := lengths(v)
			result.AddSet(newSet)
			if result.Contains(-1) {
				return result
			}
		}
		return result
	case finiteTupleType:
		if len(t) == 0 {
			result.Add(0)
			return result
		}
		thisColumnLengths := lengths((t)[0])
		remainingColumnLengths := lengths((t)[1:])
		for j := range thisColumnLengths {
			for k := range remainingColumnLengths {
				result.Add(j + k)
			}
		}
		return result
	}
	panic("We shouldn't be here!")
}

// This very similar function finds all the possible ix-th elements in a typeScheme.

func typesAtIndex(t typeScheme, ix int) alternateType {
	result, _ := recursiveTypesAtIndex(t, ix)
	return result
}

// TODO: This is somewhat wasteful because there ought to be some sensible way to fix it so the algorithm uses data from computing this for
// i - 1. But let's get the VM working first and optimise the compiler later.
func recursiveTypesAtIndex(t typeScheme, ix int) (alternateType, set.Set[int]) {
	resultTypes := alternateType{}
	resultSet := make(set.Set[int])
	switch t := t.(type) {
	case simpleType:
		if ix == 0 {
			resultTypes = resultTypes.union(alternateType{t})
		}
		resultSet.Add(1)
		return resultTypes, resultSet
	case typedTupleType:
		resultTypes = resultTypes.union(t.t)
		resultSet.Add(ix)
		return resultTypes, resultSet
	case alternateType:
		for _, v := range t {
			newTypes, newSet := recursiveTypesAtIndex(v, ix)
			resultTypes = resultTypes.union(newTypes)
			resultSet.AddSet(newSet)
		}
		return resultTypes, resultSet
	case finiteTupleType:
		if len(t) == 0 {
			return resultTypes, resultSet
		}
		resultTypes, resultSet = recursiveTypesAtIndex(t[0], ix)
		for jx := range resultSet {
			newTypes, newSet := recursiveTypesAtIndex(t[1:], ix-jx)
			resultTypes = resultTypes.union(newTypes)
			resultSet.AddSet(newSet)
		}
		return resultTypes, resultSet
	}
	panic("We shouldn't be here!")
}

type simpleType uint32

func (t simpleType) compare(u typeScheme) int {
	switch u := u.(type) {
	case simpleType:
		return int(t) - int(u)
	default:
		return -1
	}
}

type alternateType []typeScheme

func (vL alternateType) intersect(wL alternateType) alternateType {
	x := alternateType{}
	var vix, wix int
	for vix < len(vL) && wix < len(wL) {
		comp := vL[vix].compare(wL[wix])
		if comp == 0 {
			x = append(x, vL[vix])
			vix++
			wix++
			continue
		}
		if comp < 0 {
			vix++
			continue
		}
		wix++
	}
	return x
}

func (vL alternateType) union(wL alternateType) alternateType {
	x := alternateType{}
	var vix, wix int
	for vix < len(vL) || wix < len(wL) {
		if vix == len(vL) {
			x = append(x, wL[wix])
			wix++
			continue
		}
		if wix == len(wL) {
			x = append(x, vL[vix])
			vix++
			continue
		}
		comp := vL[vix].compare(wL[wix])
		if comp == 0 {
			x = append(x, vL[vix])
			vix++
			wix++
			continue
		}
		if comp < 0 {
			x = append(x, vL[vix])
			vix++
			continue
		}
		x = append(x, wL[wix])
		wix++
	}
	return x
}

func (vL alternateType) without(t typeScheme) alternateType {
	x := alternateType{}
	for _, v := range vL {
		if v.compare(t) != 0 {
			x = append(x, v)
		}
	}
	return x
}

func (alternateType alternateType) only(t simpleType) bool {
	if len(alternateType) == 1 {
		switch el := alternateType[0].(type) {
		case *simpleType:
			return *el == t
		default:
			return false
		}
	}
	return false
}

func (alternateType alternateType) contains(t simpleType) bool {
	for _, ty := range alternateType {
		switch el := ty.(type) {
		case *simpleType:
			return (*el) == t
		}
	}
	return false
}

func (t alternateType) compare(u typeScheme) int {
	switch u := u.(type) {
	case simpleType:
		return 1
	case alternateType:
		diff := len(t) - len(u)
		if diff != 0 {
			return diff
		}
		for i := 0; i < len(t); i++ {
			diff := (t)[i].compare((u)[i])
			if diff != 0 {
				return diff
			}
		}
		return 0
	default:
		return -1
	}
}

type finiteTupleType []typeScheme // "Finite" meaning that we know its size at compile time.

func (t finiteTupleType) compare(u typeScheme) int {
	switch u := u.(type) {
	case simpleType, alternateType:
		return 1
	case finiteTupleType:
		diff := len(t) - len(u)
		if diff != 0 {
			return diff
		}
		for i := 0; i < len(t); i++ {
			diff := (t)[i].compare((u)[i])
			if diff != 0 {
				return diff
			}
		}
		return 0
	default:
		return -1
	}
}

type typedTupleType struct { // We don't know how long it is but we know what its elements are. (or we can say 'single' if we don't.)
	t alternateType
}

func (t typedTupleType) compare(u typeScheme) int {
	switch u := u.(type) {
	case simpleType, finiteTupleType:
		return 1
	case typedTupleType:
		return t.t.compare(u.t)
	default:
		return -1
	}
}

func simpleList(t simpleType) alternateType {
	return alternateType{t}
}

func (v *Value) describe() string {
	switch v.T {
	case INT:
		return strconv.Itoa(v.V.(int))
	case STRING:
		return "\"" + v.V.(string) + "\""
	case BOOL:
		if v.V.(bool) {
			return "true"
		} else {
			return "false"
		}
	case FLOAT:
		return strconv.FormatFloat(v.V.(float64), 'g', 8, 64)
	case UNSAT:
		return "unsatisfied conditional"
	case NULL:
		return "null"
	case THUNK:
		return "thunk"
	}

	panic("can't describe value")
}