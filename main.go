package main

// import "syscall"
import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/cryptix/wav"
	"github.com/ironiridis/alsa"
)

var udpSocket net.PacketConn
var recordDev, playbackDev *alsa.Device

func setupNetwork() {
	u, err := net.ListenPacket("udp", "255.255.255.255:7171")
	if err != nil {
		panic(err)
	}
	udpSocket = u
}

func tx(s string) {
	broadcast, err := net.ResolveUDPAddr("udp", "255.255.255.255:7171")
	if err != nil {
		fmt.Printf("resolve to addr failed: %v\n\n", err)
		return
	}
	_, err = udpSocket.WriteTo([]byte(s), broadcast)
	if err != nil {
		fmt.Printf("tx failed: %v\n", err)
	}
}

func fail(err error) {
	// future:
	//  log error
	//  gracefully unmount (or don't, since we're r/o anyway)

	// announce to network to be logged by ... somebody
	tx(err.Error())

	// wait, then panic to reboot (since we're running at PID 1)
	time.Sleep(10 * time.Second)
	panic(err)
}

func findDevices() {
	cards, err := alsa.OpenCards()
	if err != nil {
		fail(err)
	}
	defer alsa.CloseCards(cards)

	for _, card := range cards {
		devices, err := card.Devices()
		if err != nil {
			fail(err)
		}
		if card.String() == "USB Audio Device" {
			for _, device := range devices {
				if device.Record {
					recordDev = device
				} else if device.Play {
					playbackDev = device
				}
			}
		}
	}
	if recordDev == nil || playbackDev == nil {
		fail(fmt.Errorf("couldn't find both record and playback devices"))
	}
}

func main() {
	setupNetwork()
	findDevices()

	// waveWriter("test.wav", device, time.After(time.Second*5))
	return
}

func alsaFormatBits(f alsa.FormatType) uint16 {
	switch f {
	case alsa.S8, alsa.U8:
		return 8
	case alsa.S16_LE, alsa.S16_BE, alsa.U16_LE, alsa.U16_BE:
		return 16
	case alsa.S24_LE, alsa.S24_BE, alsa.U24_LE, alsa.U24_BE:
		return 24
	case alsa.S32_LE, alsa.S32_BE, alsa.U32_LE, alsa.U32_BE:
		return 32
	case alsa.FLOAT_LE, alsa.FLOAT_BE:
		return 32 // guess
	case alsa.FLOAT64_LE, alsa.FLOAT64_BE:
		return 64
	}

	fail(fmt.Errorf("unknown alsa format: %+v", f))
	return 0
}

func waveWriter(fn string, d *alsa.Device, done <-chan time.Time) {
	var err error
	if err = d.Open(); err != nil {
		fail(fmt.Errorf("err=%v, path=%v", err, d.Path))
	}
	defer d.Close()

	capChans, err := d.NegotiateChannels(1)
	if err != nil {
		fail(err)
	}
	capRate, err := d.NegotiateRate(44100)
	if err != nil {
		fail(err)
	}
	capFormat, err := d.NegotiateFormat(alsa.S16_LE)
	if err != nil {
		fail(err)
	}
	capBuffer, err := d.NegotiateBufferSize(8192, 16384)
	if err != nil {
		fail(err)
	}
	if err = d.Prepare(); err != nil {
		fail(err)
	}
	capBPF := d.BytesPerFrame()

	wavHdr := wav.File{
		Channels:        uint16(capChans),
		SampleRate:      uint32(capRate),
		SignificantBits: alsaFormatBits(capFormat),
	}
	f, err := os.Create(fn)
	if err != nil {
		fail(err)
	}

	wavEnc, err := wavHdr.NewWriter(f)
	if err != nil {
		fail(err)
	}
	defer wavEnc.Close()

	buf := make([]byte, capBPF*capBuffer)
	for {
		select {
		case <-done:
			return

		default:
			if err = d.Read(buf, len(buf)/capBPF); err != nil {
				fail(err)
			}
			_, err = wavEnc.Write(buf)
			if err != nil {
				fail(err)
			}
		}
	}
}
