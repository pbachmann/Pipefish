snap: good
script: rsc/suite/manual-2.ch

-> h
"Hello world!"
-> x
4
-> MONTHS_IN_A_YEAR
12
-> MONTHS_IN_A_YEAR = 13
error "reassigning to a constant 'MONTHS_IN_A_YEAR' in the REPL"
-> h = 42
error "attempting to assign object of type 'int' to a variable of type 'string'"
-> h = "foo"
ok
-> addToXandShow 38
error "can't find function 'addToXandShow'"
-> addToXAndShow 38
42

-> X
"hello world"
-> Y
1, 2, 3, 4, 5, 6