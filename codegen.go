package main

import (
	"fmt"
	"os"
	"strings"
)

func codegen() {
	fmt.Println("请输入编码:")
	var code string
	fmt.Scanln(&code)
	code = strings.ToUpper(strings.TrimSpace(code))
	codeSplit := strings.Split(code, "")
	for _, cv := range codeSplit {
		if !checkByString(cv) {
			fmt.Printf("\n包含非法字符【%s】请重试\n", cv)
			fmt.Scanln(&code)
		}
	}
	// fmt.Println("您输入了:", code, len(codeSplit), codeSplit)
	nextCode := getNextCode(codeSplit)
	fmt.Printf("\nNext Code is: %v \n", nextCode)
	codes := getNextCodeList([]string{nextCode})
	f, err := os.OpenFile("codes.csv", os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0755)
	if err != nil {
		fmt.Printf("error for os.Openfile: %v", err)
		fmt.Scanln(&code)
	}
	fmt.Println("\nNext Codes List is: ")
	for _, c := range codes {
		fmt.Printf("%s\n", c)
		f.WriteString(c + "\n")
	}
	defer f.Close()
	fmt.Scanln(&code)
}

func getNextCodeList(codes []string) []string {
	code := codes[len(codes)-1]
	codeSplit := strings.Split(code, "")
	if len(codes) == 30 {
		return codes
	}
	nextCode := getNextCode(codeSplit)
	codes = append(codes, nextCode)
	return getNextCodeList(codes)
}

func getNextCode(codeSplit []string) string {
	codesstr, codeslist := getCodesList()
	// fmt.Println("codeslist=", codeslist)
	// fmt.Println("codesstr=", codesstr)
	j := len(codeSplit) - 1
	nextPlus1 := false
	lastIndex := len(codeslist) - 1
	// fmt.Println("lastIndex=", lastIndex)
	for j > -1 {
		bb := codeSplit[j]
		index := strings.Index(codesstr, bb)
		// fmt.Printf(" %s CurrentIndex=%d ", bb, index)
		nextIndex := 0
		if j == (len(codeSplit) - 1) {
			if index == lastIndex {
				nextPlus1 = true
			} else {
				nextIndex = index + 1
			}
		} else {
			nextIndex = index
			if nextPlus1 {
				if nextIndex == lastIndex {
					nextIndex = 0
					nextPlus1 = true
				} else {
					nextIndex += 1
					nextPlus1 = false
				}
			}
		}
		// fmt.Printf("NextIndex=%d ", nextIndex)
		codeSplit[j] = codeslist[nextIndex]
		// fmt.Println("----j=", j)
		j--
	}
	result := ""
	for _, vvv := range codeSplit {
		result += vvv
	}
	return result
}

func getCodesList() (codesstr string, codeslist []string) {
	// 1, 2, 0, O, Q, I, Z
	a := '3'
	var codes []rune
	for a <= 'Z' {
		codes = append(codes, a)
		if a == 'Z' {
			break
		}
		if a == '9' {
			a = 'A'
		} else {
			a += 1
		}
		if !checkByRune(a) {
			a += 1
		}
	}
	for _, b := range codes {
		sstr := fmt.Sprintf("%c", b)
		codeslist = append(codeslist, sstr)
		codesstr += sstr
	}
	return
}

func checkByRune(a rune) bool {
	if a == 'O' || a == 'Q' || a == 'I' || a == 'Z' || a == '0' || a == '1' || a == '2' {
		return false
	}
	return true
}

func checkByString(a string) bool {
	if a == "O" || a == "Q" || a == "I" || a == "Z" || a == "0" || a == "1" || a == "2" {
		return false
	}
	return true
}
