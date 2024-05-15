package service

import (
	"pipefish/source/parser"
	"pipefish/source/report"
	"pipefish/source/settings"
	"pipefish/source/token"
	"pipefish/source/values"
	"strconv"
	"strings"
)

// This is what initialization constructs: a vm and a compiler that between them can evaluate a line of Pipefish.
type VmService struct {
	Mc      *Vm
	Cp      *Compiler // This also contains all the metadata about the top-level source code.
	Broken  bool
	Visited bool
}

func (service *VmService) NeedsUpdate() (bool, error) {
	return service.Cp.NeedsUpdate()
}

// We have two types of external service, defined below: one for services on the same hub, one for services on
// a different hub. Eventually we will need a third class of things on a different hub of the same instance of
// Pipefish, but we haven't implemented that in general yet.
type externalService interface {
	evaluate(mc *Vm, line string) values.Value
	getResolvingParser() *parser.Parser
	problem() *report.Error
	getAPI() string
}

type externalServiceOnSameHub struct {
	externalService *VmService
}

// There is a somewhat faster way of doing this when the services are on the same hub, since we would just need
// to change the type numbers. TODO. Until then, this serves as a good test bed for the external services on other hubs.
func (ex externalServiceOnSameHub) evaluate(mc *Vm, line string) values.Value {
	exVal := ex.externalService.Cp.Do(ex.externalService.Mc, line)
	serialize := ex.externalService.Mc.Literal(exVal)
	return mc.OwnService.Cp.Do(mc, serialize)
}

func (ex externalServiceOnSameHub) getResolvingParser() *parser.Parser {
	return ex.externalService.Cp.P
}

func (es externalServiceOnSameHub) problem() *report.Error {
	if es.externalService.Broken {
		return report.CreateErr("ext/broken", &token.Token{})
	}
	return nil
}

func (es externalServiceOnSameHub) getAPI() string {
	return es.externalService.SerializeApi()
}

type externalServiceOnDifferentHub struct {
	username string
	password string
}

func (es externalServiceOnDifferentHub) evaluate(mc *Vm, line string) values.Value {
	return values.Value{values.NULL, nil}
}

func (eS externalServiceOnDifferentHub) getResolvingParser() *parser.Parser {
	return nil
}

func (es externalServiceOnDifferentHub) problem() *report.Error {
	return nil
}

func (es externalServiceOnDifferentHub) getAPI() string {
	return ""
}

// For a description of the file format, see README-api-serialization.md
func (service VmService) SerializeApi() string {
	var buf strings.Builder
	for i := values.LB_ENUMS; i < service.Mc.Ub_enums; i++ {
		enumOrdinal := i - values.LB_ENUMS
		if service.Mc.typeAccess[i] == PUBLIC && !service.isMandatoryImport(enumDeclaration, int(enumOrdinal)) {
			buf.WriteString("ENUM | ")
			buf.WriteString(service.Mc.concreteTypeNames[i])
			for _, el := range service.Mc.Enums[i-values.LB_ENUMS] {
				buf.WriteString(" | ")
				buf.WriteString(el)
			}
			buf.WriteString("\n")
		}
	}

	for i := service.Mc.Ub_enums; i < service.Mc.Lb_snippets; i++ {
		structOrdinal := i - service.Mc.Ub_enums
		if service.Mc.typeAccess[i] == PUBLIC && !service.isMandatoryImport(structDeclaration, int(structOrdinal)) {
			buf.WriteString("STRUCT | ")
			buf.WriteString(service.Mc.concreteTypeNames[i])
			labels := service.Mc.StructLabels[structOrdinal]
			for i, lb := range labels { // We iterate by the label and not by the value so that we can have hidden fields in the structs, as we do for efficiency when making a compilable snippet.
				buf.WriteString(" | ")
				buf.WriteString(service.Mc.Labels[lb])
				buf.WriteString(" ")
				buf.WriteString(service.serializeAbstractType(service.Mc.AbstractStructFields[structOrdinal][i]))
			}
			buf.WriteString("\n")
		}
	}

	for i := len(nativeAbstractTypes); i < len(service.Mc.AbstractTypes); i++ {
		ty := service.Mc.AbstractTypes[i]
		if !(service.isPrivate(ty.AT)) && !service.isMandatoryImport(abstractDeclaration, i) {
			buf.WriteString("ABSTRACT | ")
			buf.WriteString(ty.Name)
			buf.WriteString(" | ")
			buf.WriteString(service.serializeAbstractType(ty.AT))
		}
		buf.WriteString("\n")
	}

	for name, fns := range service.Cp.P.FunctionTable {
		for defOrCmd := 0; defOrCmd < 2; defOrCmd++ { // In the function table the commands and functions are all jumbled up. But we want the commands first, for neatness, so we'll do two passes.
			for _, fn := range fns {
				if fn.Private || settings.MandatoryImportSet.Contains(fn.Body.GetToken().Source) {
					continue
				}
				if fn.Cmd {
					if defOrCmd == 1 {
						continue
					}
					buf.WriteString("COMMAND | ")
				} else {
					if defOrCmd == 0 {
						continue
					}
					buf.WriteString("FUNCTION | ")
				}
				buf.WriteString(name)
				buf.WriteString(" | ")
				buf.WriteString(strconv.Itoa(int(fn.Position)))
				for _, ntp := range fn.Sig {
					buf.WriteString(" | ")
					buf.WriteString(ntp.VarName)
					buf.WriteString(" ")
					buf.WriteString(ntp.VarType)
				}
				buf.WriteString(" | ")
				buf.WriteString(service.serializeTypescheme(service.Cp.Fns[fn.Number].Types))
				buf.WriteString("\n")
			}
		}
	}
	return buf.String()
}

func (service *VmService) isMandatoryImport(dec declarationType, ordinal int) bool {
	return settings.MandatoryImportSet.Contains(service.Cp.P.ParsedDeclarations[dec][ordinal].GetToken().Source)
}

func (service *VmService) isPrivate(a values.AbstractType) bool { // TODO --- obviously this only needs calculating once and sticking in the compiler.
	for _, w := range a.Types {
		if service.Mc.typeAccess[w] == PRIVATE {
			return true
		}
	}
	return false
}

// And then we need a way to turn a serialized API back into a set of declarations.
// xserve is the external service number: set to DUMMY it will indicate that we're just doing this for human readers and
// can therefore leave off the 'xcall' hooks.
func SerializedAPIToDeclarations(serializedAPI string, xserve uint32) string {
	var buf strings.Builder
	lines := strings.Split(strings.TrimRight(serializedAPI, "\n"), "\n")
	lineNo := 0
	hasHappened := map[string]bool{"ENUM": false, "STRUCT": false, "ABSTRACT": false, "COMMAND": false, "FUNCTION": false}
	for lineNo < len(lines) {
		line := lines[lineNo]
		parts := strings.Split(line, " | ")
		switch parts[0] {
		case "ENUM":
			if !hasHappened["ENUM"] {
				buf.WriteString("newtype\n\n")
			}
			buf.WriteString(parts[1])
			buf.WriteString(" = enum ")
			buf.WriteString(strings.Join(strings.Split(parts[2], " "), ", "))
			buf.WriteString("\n")
			lineNo++
		case "STRUCT":
			if hasHappened["ENUM"] && !hasHappened["STRUCT"] {
				buf.WriteString("\n")
			}
			if !hasHappened["ENUM"] && !hasHappened["STRUCT"] {
				buf.WriteString("newtype\n\n")
			}
			buf.WriteString(parts[1])
			buf.WriteString(" = struct (")
			sep := ""
			for _, param := range parts[2:] {
				buf.WriteString(sep)
				fieldNameAndTypes := strings.Split(param, " ")
				buf.WriteString(fieldNameAndTypes[0])
				buf.WriteString(" ")
				buf.WriteString(strings.Join(fieldNameAndTypes[1:], "/"))
				sep = ", "
			}
			buf.WriteString(")\n")
			lineNo++
		case "ABSTRACT":
			if (hasHappened["ENUM"] || hasHappened["STRUCT"]) && !hasHappened["ABSTRACT"] {
				buf.WriteString("\n")
			}
			if !hasHappened["ENUM"] && !hasHappened["STRUCT"] && !hasHappened["ABSTRACT"] {
				buf.WriteString("newtype\n\n")
			}
			buf.WriteString(parts[1])
			buf.WriteString(" = ")
			buf.WriteString(strings.Join(strings.Split(parts[2], " "), "/"))
			lineNo++
		case "COMMAND":
			if !hasHappened["CMD"] {
				buf.WriteString("\ncmd\n\n")
			}
			buf.WriteString(makeCommandOrFunctionDeclarationFromParts(parts[1:], xserve))
			lineNo++
		case "FUNCTION":
			if !hasHappened["DEF"] {
				buf.WriteString("\ndef\n\n")
			}
			buf.WriteString(makeCommandOrFunctionDeclarationFromParts(parts[1:], xserve))
			lineNo++
		case "":
			lineNo++
		default:
			panic("Oops, found" + parts[0] + "instead. Drat.")
		}
		hasHappened[parts[0]] = true
	}
	return buf.String()
}

func makeCommandOrFunctionDeclarationFromParts(parts []string, xserve uint32) string {
	var buf strings.Builder
	// We have snipped off the part saying "FUNCTION" or "COMMAND", so the list of parts now looks like this:
	// functionName | 0, 1, 2, 3 for prefix/infix/suffix/unfix | parameterName1 type1 | parameterName2 type2 | serialization of typescheme
	// We can't use the serialization of the typescheme here, so we can break it down into parts:
	functionName := parts[0]
	posInt, _ := strconv.Atoi(parts[1])
	position := uint32(posInt)
	params := parts[2 : len(parts)-1]
	if position == UNFIX {
		return functionName
	}
	if position == PREFIX {
		buf.WriteString(functionName)
		buf.WriteString(" ")
	}
	buf.WriteString("(")
	lastWasBling := false
	for i, param := range params {
		bits := strings.Split(param, " ")
		if bits[1] == "bling" {
			if !lastWasBling {
				buf.WriteString(")")
			}
			buf.WriteString(" ")
			buf.WriteString(bits[0])
			lastWasBling = true
			continue
		}
		// So it's non-bling
		if lastWasBling {
			buf.WriteString(" (")
		} else {
			if i > 0 {
				buf.WriteString(", ")
			}
		}
		buf.WriteString(bits[0])
		buf.WriteString(" ")
		buf.WriteString(bits[1])
	}
	buf.WriteString(")")
	if position == SUFFIX {
		buf.WriteString(" ")
		buf.WriteString(functionName)
	}
	if xserve != DUMMY { // Then we need to insert the hook.
		buf.WriteString(" : xcall ")
		buf.WriteString(strconv.Itoa(int(xserve)))
		buf.WriteString(", ")
		buf.WriteString("\"")
		buf.WriteString(functionName)
		buf.WriteString("\"")
		buf.WriteString(", ")
		buf.WriteString(strconv.Itoa(int(position)))
		buf.WriteString(", ")
		buf.WriteString("\"")
		buf.WriteString(parts[len(parts)-1])
		buf.WriteString("\"")
	}
	buf.WriteString("\n")
	return buf.String()
}

func (service *VmService) serializeAbstractType(ty values.AbstractType) string {
	return strings.ReplaceAll(service.Mc.DescribeAbstractType(ty), "/", " ")
}

// The compiler infers more about the return types of a function than is expressed in the code or
// indeed expressible in Pipefish. We will turn the typescheme into a serialized description in Reverse
// Polish notation.
func (service *VmService) serializeTypescheme(t typeScheme) string {
	switch t := t.(type) {
	case simpleType:
		return service.Mc.concreteTypeNames[t]
	case TypedTupleType:
		acc := ""
		for _, u := range t.T {
			acc = acc + service.serializeTypescheme(u) + " "
		}
		acc = acc + "*TT " + strconv.Itoa(len(t.T))
		return acc
	case AlternateType:
		acc := ""
		for _, u := range t {
			acc = acc + service.serializeTypescheme(u) + " "
		}
		acc = acc + "*AT " + strconv.Itoa(len(t))
		return acc
	case finiteTupleType:
		acc := ""
		for _, u := range t {
			acc = acc + service.serializeTypescheme(u) + " "
		}
		acc = acc + "*FT " + strconv.Itoa(len(t))
		return acc
	}
	panic("Unhandled type scheme!")
}
