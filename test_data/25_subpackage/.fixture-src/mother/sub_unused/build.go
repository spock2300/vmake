package main

// This sub-package must never be loaded: it is not referenced by any build
// script. The invalid syntax proves that discovery stays lazy.
func Main( {
