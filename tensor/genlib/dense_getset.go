package main

import (
	"fmt"
	"io"
	"text/template"
)

const memsetRaw = `// Memset sets all values in the *Dense tensor to x.
func (t *Dense) Memset(x interface{}) error {
	if t.IsMaterializable() {
		return t.memsetIter(x)
	}
	switch t.t.Kind() {
	{{range .Kinds -}}
		{{if isParameterized . -}}
		{{else -}}
	case reflect.{{reflectKind .}}:
		xv, ok := x.({{asType .}})
		if !ok {
			return errors.Errorf(dtypeMismatch, t.t, x)
		}
		data := t.{{sliceOf .}}
		for i := range data{
			data[i] = xv
		}
		{{end -}}
	{{end -}}
	default:
		xv := reflect.ValueOf(x)
		ptr := uintptr(t.ptr)
		for i := 0; i < t.l; i++ {
			want := ptr + uintptr(i)*t.t.Size()
			val := reflect.NewAt(t.t, unsafe.Pointer(want))
			val = reflect.Indirect(val)
			val.Set(xv)
		}
	}
	return nil
}

func (t *Dense) memsetIter(x interface{}) (err error) {
	it := NewFlatIterator(t.AP)
	var i int
	switch t.t.Kind() {
	{{range .Kinds -}}
		{{if isParameterized . -}}
		{{else -}}
	case reflect.{{reflectKind .}}:
		xv, ok := x.({{asType .}})
		if !ok {
			return errors.Errorf(dtypeMismatch, t.t, x)
		}
		data := t.{{sliceOf .}}
		for i, err = it.Next(); err == nil; i, err = it.Next(){
			data[i] = xv	
		}
		err = handleNoOp(err)
		{{end -}}
	{{end -}}
	default:
		xv := reflect.ValueOf(x)
		ptr := uintptr(t.ptr)
		for i, err = it.Next(); err == nil; i, err = it.Next(){
			want := ptr + uintptr(i)*t.t.Size()
			val := reflect.NewAt(t.t, unsafe.Pointer(want))
			val = reflect.Indirect(val)
			val.Set(xv)
		}
		err = handleNoOp(err)
	}
	return
}

`

const zeroRaw = `// Zero zeroes out the underlying array of the *Dense tensor
func (t *Dense) Zero() {
	if t.IsMaterializable() {
		if err := t.zeroIter(); err !=nil {
			panic(err)
		}
	}
	if t.IsMasked(){
		t.ResetMask()
	}
	switch t.t.Kind() {
	{{range .Kinds -}}
		{{if isParameterized . -}}
		{{else -}}
	case reflect.{{reflectKind .}}:
		data := t.{{sliceOf .}}
		for i := range data {
			data[i] = {{if eq .String "bool" -}}
				false
			{{else if eq .String "string" -}}""
			{{else if eq .String "unsafe.Pointer" -}}nil
			{{else -}}0{{end}}
		}
		{{end -}}
	{{end -}}
	default:
		ptr := uintptr(t.ptr)
		for i := 0; i < t.l; i++ {
			want := ptr + uintptr(i)*t.t.Size()
			val := reflect.NewAt(t.t, unsafe.Pointer(want))
			val = reflect.Indirect(val)
			val.Set(reflect.Zero(t.t))
		}
	}
}

func (t *Dense) zeroIter() (err error){
	it := NewFlatIterator(t.AP)
	var i int
	switch t.t.Kind() {
	{{range .Kinds -}}
		{{if isParameterized . -}}
		{{else -}}
	case reflect.{{reflectKind .}}:
		data := t.{{sliceOf .}}
		for i, err = it.Next(); err == nil; i, err = it.Next(){
			data[i] = {{if eq .String "bool" -}}
				false
			{{else if eq .String "string" -}}""
			{{else if eq .String "unsafe.Pointer" -}}nil
			{{else -}}0{{end}}
		}
		err = handleNoOp(err)
		{{end -}}
	{{end -}}
	default:
		ptr := uintptr(t.ptr)
		for i, err = it.Next(); err == nil; i, err = it.Next(){
			want := ptr + uintptr(i)*t.t.Size()
			val := reflect.NewAt(t.t, unsafe.Pointer(want))
			val = reflect.Indirect(val)
			val.Set(reflect.Zero(t.t))
		}
		err = handleNoOp(err)
	}
	return
}
`

const copyRaw = `func copyDense(dest, src *Dense) int {
	if dest.t != src.t {
		err := errors.Errorf(dtypeMismatch, src.t, dest.t)
		panic(err.Error())
	}
	if src.IsMasked(){
		if cap(dest.mask)<len(src.mask){
			dest.mask=make([]bool, len(src.mask))
		}
		copy(dest.mask, src.mask)
		dest.mask=dest.mask[:len(src.mask)]
	}
	switch dest.t.Kind() {
	{{range .Kinds -}}
		{{if isParameterized .}}
		{{else -}}
	case reflect.{{reflectKind .}}:
		return copy(dest.{{sliceOf .}}, src.{{sliceOf .}})
		{{end -}}
	{{end -}}
	default:
		dv := reflect.ValueOf(dest.v)
		sv := reflect.ValueOf(src.v)
		return reflect.Copy(dv, sv)
	}
}
`

const copySlicedRaw = `func copySliced(dest *Dense, dstart, dend int, src *Dense, sstart, send int) int{
	if dest.t != src.t {
		panic("Cannot copy arrays of different types")
	}

	if src.IsMasked(){
		mask:=dest.mask
		if cap(dest.mask) < dend{
			mask = make([]bool, dend)
		}
		copy(mask, dest.mask)
		dest.mask=mask
		copy(dest.mask[dstart:dend], src.mask[sstart:send])
	}
	switch dest.t.Kind() {
	{{range .Kinds -}}
		{{if isParameterized .}}
		{{else -}}
	case reflect.{{reflectKind .}}:
		return copy(dest.{{sliceOf .}}[dstart:dend], src.{{sliceOf .}}[sstart:send])
		{{end -}}
	{{end -}}
	default:
		dv := reflect.ValueOf(dest.v)
		dv = dv.Slice(dstart, dend)
		sv := reflect.ValueOf(src.v)
		sv = sv.Slice(sstart, send)
		return reflect.Copy(dv, sv)
	}	
}
`

const copyIterRaw = `func copyDenseIter(dest, src *Dense, diter, siter *FlatIterator) (int, error) {
	if dest.t != src.t {
		panic("Cannot copy arrays of different types")
	}

	if diter == nil && siter == nil && !dest.IsMaterializable() && !src.IsMaterializable() {
		return copyDense(dest, src), nil
	}

	if diter == nil {
		diter = NewFlatIterator(dest.AP)	
	}
	if siter == nil {
		siter = NewFlatIterator(src.AP)
	}
	
	isMasked:= src.IsMasked()
	if isMasked{
		if cap(dest.mask)<src.DataSize(){
			dest.mask=make([]bool, src.DataSize())
		}
		dest.mask=dest.mask[:dest.DataSize()]
	}

	k := dest.t.Kind()
	var i, j, count int
	var err error
	for {
		if i, err = diter.Next() ; err != nil {
			if err = handleNoOp(err); err != nil{
				return count, err
			}
			break
		}
		if j, err = siter.Next() ; err != nil {
			if err = handleNoOp(err); err != nil{
				return count, err
			}
			break
		}
		if isMasked{
			dest.mask[i]=src.mask[j]
		}
		
		switch k {
		{{range .Kinds -}}
			{{if isParameterized . -}}
			{{else -}}
		case reflect.{{reflectKind .}}:
			dest.set{{short .}}(i, src.get{{short .}}(j))
			{{end -}}
		{{end -}}
		default:
			dest.Set(i, src.Get(j))
		}
		count++
	}
	return count, err
}
`

const sliceRaw = `// the method assumes the AP and metadata has already been set and this is simply slicing the values
func (t *Dense) slice(start, end int) {
	switch t.t.Kind() {
	{{range .Kinds -}}
		{{if isParameterized .}}
		{{else -}}
	case reflect.{{reflectKind .}}:
		data := t.{{sliceOf .}}[start:end]
		t.fromSlice(data)
		{{end -}}
	{{end -}}
	default:
		v := reflect.ValueOf(t.v)
		v = v.Slice(start, end)
		t.fromSlice(v.Interface())
	}	
}
`

const denseEqRaw = `// Eq checks that any two things are equal. If the shapes are the same, but the strides are not the same, it's will still be considered the same
func (t *Dense) Eq(other interface{}) bool {
	if ot, ok := other.(*Dense); ok {
		if ot == t {
			return true
		}

		if ot.len() != t.len() {
			return false
		}

		if t.t != ot.t {
			return false
		}

		if !t.Shape().Eq(ot.Shape()) {
			return false
		}

		switch t.t.Kind() {
		{{range .Kinds -}}
			{{if isParameterized . -}}
			{{else -}}
		case reflect.{{reflectKind .}}:
			for i, v := range t.{{sliceOf .}} {
				if ot.get{{short .}}(i) != v {
					return false
				}
			}
			{{end -}}
		{{end -}}
		default:
			for i := 0; i < t.len(); i++{
				if !reflect.DeepEqual(t.Get(i), ot.Get(i)){
					return false
				}
			}
		}
		return true
	}
	return false
}
`

var (
	Memset     *template.Template
	Zero       *template.Template
	Copy       *template.Template
	CopySliced *template.Template
	CopyIter   *template.Template
	Slice      *template.Template
	Eq         *template.Template
)

func init() {
	Memset = template.Must(template.New("Memset").Funcs(funcs).Parse(memsetRaw))
	Zero = template.Must(template.New("Zero").Funcs(funcs).Parse(zeroRaw))

	Copy = template.Must(template.New("copy").Funcs(funcs).Parse(copyRaw))
	CopySliced = template.Must(template.New("copySliced").Funcs(funcs).Parse(copySlicedRaw))
	CopyIter = template.Must(template.New("copyIter").Funcs(funcs).Parse(copyIterRaw))
	Slice = template.Must(template.New("slice").Funcs(funcs).Parse(sliceRaw))
	Eq = template.Must(template.New("eq").Funcs(funcs).Parse(denseEqRaw))
}

func getset(f io.Writer, generic *ManyKinds) {
	fmt.Fprintf(f, "\n\n\n")
	fmt.Fprintf(f, "\n\n\n")
	Memset.Execute(f, generic)
	fmt.Fprintf(f, "\n\n\n")
	Zero.Execute(f, generic)
	fmt.Fprintf(f, "\n\n\n")
	Copy.Execute(f, generic)
	fmt.Fprintf(f, "\n\n\n")
	CopySliced.Execute(f, generic)
	fmt.Fprintf(f, "\n\n\n")
	CopyIter.Execute(f, generic)
	fmt.Fprintf(f, "\n\n\n")
	Slice.Execute(f, generic)
	fmt.Fprintf(f, "\n\n\n")
	Eq.Execute(f, generic)
}
