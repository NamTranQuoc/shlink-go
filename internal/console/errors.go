package console

import (
	"errors"
	"fmt"
)

// Exception is an error rendered the way Application::renderThrowable() renders PHP exceptions. Console exceptions
// (those implementing Symfony's ExceptionInterface) are rendered without their "In <file> line <n>:" header unless
// the output is verbose.
type Exception interface {
	error
	// PHPClass is the upstream exception class, shown in verbose output.
	PHPClass() string
	// PHPLocation returns the upstream file basename and line the exception is thrown from.
	PHPLocation() (file string, line int)
	// IsConsoleException tells if the exception implements Symfony's Console ExceptionInterface.
	IsConsoleException() bool
	// Code is the exception code, used as the process exit code.
	Code() int
}

// consoleError carries a Symfony Console exception.
type consoleError struct {
	class   string
	message string
	file    string
	line    int
}

func (e *consoleError) Error() string              { return e.message }
func (e *consoleError) PHPClass() string           { return e.class }
func (e *consoleError) PHPLocation() (string, int) { return e.file, e.line }
func (e *consoleError) IsConsoleException() bool   { return true }
func (e *consoleError) Code() int                  { return 0 }
func newRuntimeError(message, file string, line int) *consoleError {
	return &consoleError{class: `Symfony\Component\Console\Exception\RuntimeException`, message: message, file: file, line: line}
}

// InvalidArgumentError ports Symfony's Console InvalidArgumentException.
type InvalidArgumentError struct {
	Message string
	File    string
	Line    int
}

func (e *InvalidArgumentError) Error() string { return e.Message }
func (e *InvalidArgumentError) PHPClass() string {
	return `Symfony\Component\Console\Exception\InvalidArgumentException`
}
func (e *InvalidArgumentError) PHPLocation() (string, int) { return e.File, e.Line }
func (e *InvalidArgumentError) IsConsoleException() bool   { return true }
func (e *InvalidArgumentError) Code() int                  { return 0 }

// CommandNotFoundError ports CommandNotFoundException (and NamespaceNotFoundException).
type CommandNotFoundError struct {
	Message      string
	Alternatives []string
	Namespace    bool
	line         int
}

func (e *CommandNotFoundError) Error() string { return e.Message }
func (e *CommandNotFoundError) PHPClass() string {
	if e.Namespace {
		return `Symfony\Component\Console\Exception\NamespaceNotFoundException`
	}
	return `Symfony\Component\Console\Exception\CommandNotFoundException`
}
func (e *CommandNotFoundError) PHPLocation() (string, int) { return "Application.php", e.line }
func (e *CommandNotFoundError) IsConsoleException() bool   { return true }
func (e *CommandNotFoundError) Code() int                  { return 0 }

// MissingInputError ports MissingInputException.
type MissingInputError struct{ Message string }

func (e *MissingInputError) Error() string { return e.Message }
func (e *MissingInputError) PHPClass() string {
	return `Symfony\Component\Console\Exception\MissingInputException`
}
func (e *MissingInputError) PHPLocation() (string, int) {
	return "QuestionHelper.php", 153
}
func (e *MissingInputError) IsConsoleException() bool { return true }
func (e *MissingInputError) Code() int                { return 0 }

// PHPError is a non-console exception thrown by a command, like Shlink's domain exceptions.
type PHPError struct {
	Class   string
	Message string
	File    string
	Line    int
	ExCode  int
}

func (e *PHPError) Error() string              { return e.Message }
func (e *PHPError) PHPClass() string           { return e.Class }
func (e *PHPError) PHPLocation() (string, int) { return e.File, e.Line }
func (e *PHPError) IsConsoleException() bool   { return false }
func (e *PHPError) Code() int                  { return e.ExCode }

// asException converts any error into an Exception; plain Go errors are rendered as an internal error.
func asException(err error) Exception {
	var ex Exception
	if errors.As(err, &ex) {
		return ex
	}
	return &PHPError{Class: "RuntimeException", Message: fmt.Sprint(err)}
}

// NewConsoleError creates an exception implementing Symfony's Console ExceptionInterface, like
// InvalidOptionException, thrown from the given upstream file and line.
func NewConsoleError(class, message, file string, line int) error {
	return &consoleError{class: class, message: message, file: file, line: line}
}

// PHPFatalError is a PHP Error (TypeError, ValueError, ...) thrown by a command. Application::run() does not catch
// errors by default, so PHP prints them as an uncaught fatal error on the standard output and exits with 255.
type PHPFatalError struct {
	// Output is PHP's complete fatal error report.
	Output string
}

func (e *PHPFatalError) Error() string { return e.Output }
