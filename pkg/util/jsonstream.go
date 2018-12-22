package term

import (
	"encoding/json"
	"fmt"
	"io"
)

type JSONError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type JSONMessage struct {
	Status          string     `json:"status,omitempty"`
	ID              string     `json:"id,omitempty"`
	From            string     `json:"from,omitempty"`
	Time            int64      `json:"time,omitempty"`
	TimeNano        int64      `json:"timeNano,omitempty"`
	ProgressMessage string     `json:"progress,omitempty"`
	Error           *JSONError `json:"errorDetail,omitempty"`
}

func (jm *JSONMessage) Display(out io.Writer) error {

	if jm.Error != nil {
		fmt.Fprintf(out, "error pulling image, %s\n\r", jm.Error.Message)
		return nil
	}
	// do not display progress bar
	if len(jm.ProgressMessage) > 0 {
		return nil
	}
	fmt.Fprintf(out, "\t%s %s \n\r", jm.Status, jm.ID)

	return nil
}

// DisplayJsonStream parse the json input from `in` and pipe the necessary message to `out`
func DisplayDockerJsonStream(in io.Reader, out io.Writer) error {

	var (
		dec = json.NewDecoder(in)
	)

	for {
		var jm JSONMessage
		if err := dec.Decode(&jm); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		err := jm.Display(out)
		if err != nil {
			return err
		}
	}

	return nil
}
