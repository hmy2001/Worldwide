package emulator

import "fmt"

// FetchMemory8 引数で指定したアドレスから値を取得する
func (cpu *CPU) FetchMemory8(addr uint16) byte {
	var value byte

	switch {
	case addr >= 0x4000 && addr < 0x8000:
		// ROMバンク
		value = cpu.ROMBank[cpu.ROMBankPtr][addr-0x4000]
	case addr >= 0x8000 && addr < 0xa000:
		// VRAMバンク
		value = cpu.GPU.VRAMBank[cpu.GPU.VRAMBankPtr][addr-0x8000]
	case addr >= 0xa000 && addr < 0xc000:
		if cpu.RTC.Mapped != 0 {
			value = cpu.RTC.Read(byte(cpu.RTC.Mapped))
		} else {
			// RAMバンク
			value = cpu.RAMBank[cpu.RAMBankPtr][addr-0xa000]
		}
	case cpu.WRAMBankPtr > 1 && addr >= 0xd000 && addr < 0xe000:
		// WRAMバンク
		value = cpu.WRAMBank[cpu.WRAMBankPtr][addr-0xd000]
	case (addr >= 0xff10 && addr <= 0xff26) || (addr >= 0xff30 && addr <= 0xff3f):
		// サウンドアクセス
		value = cpu.Sound.Read(addr)
	case addr == LCDCIO:
		value = cpu.GPU.LCDC
	case addr == LCDSTATIO:
		value = cpu.GPU.LCDSTAT
	case addr == BCPDIO:
		// 背景パレットデータ読み込み
		index := cpu.GPU.FetchBGPalleteIndex()
		value = cpu.GPU.BGPallete[index]
	case addr == OCPDIO:
		// スプライトパレットデータ読み込み
		index := cpu.GPU.FetchSPRPalleteIndex()
		value = cpu.GPU.SPRPallete[index]
	default:
		value = cpu.RAM[addr]
	}
	return value
}

// SetMemory8 引数で指定したアドレスにvalueを書き込む
func (cpu *CPU) SetMemory8(addr uint16, value byte) {
	if addr <= 0x7fff {
		// ROM領域
		if (addr >= 0x2000) && (addr <= 0x3fff) {
			switch cpu.Cartridge.MBC {
			case "MBC1":
				// ROMバンク下位5bit
				if value == 0 {
					value = 1
				}
				upper2 := cpu.ROMBankPtr >> 5
				lower5 := value
				newROMBankPtr := (upper2 << 5) | lower5
				cpu.switchROMBank(newROMBankPtr)
			case "MBC3":
				newROMBankPtr := value & 0x7f
				if newROMBankPtr == 0 {
					newROMBankPtr = 1
				}
				cpu.switchROMBank(newROMBankPtr)
			}
		} else if (addr >= 0x4000) && (addr <= 0x5fff) {
			switch cpu.Cartridge.MBC {
			case "MBC1":
				// RAM バンク番号または、 ROM バンク番号の上位ビット
				if cpu.bankMode == 0 {
					// ROMptrの上位2bitの切り替え
					upper2 := value
					lower5 := cpu.ROMBankPtr & 0x1f
					newROMBankPtr := (upper2 << 5) | lower5
					cpu.switchROMBank(newROMBankPtr)
				} else if cpu.bankMode == 1 {
					// RAMptrの切り替え
					newRAMBankPtr := value
					cpu.RAMBankPtr = newRAMBankPtr
				}
			case "MBC3":
				switch {
				case value <= 0x07:
					cpu.RTC.Mapped = 0
					cpu.RAMBankPtr = value
				case value >= 0x08 && value <= 0x0c:
					cpu.RTC.Mapped = uint(value)
				}
			}
		} else if (addr >= 0x6000) && (addr <= 0x7fff) {
			switch cpu.Cartridge.MBC {
			case "MBC1":
				// ROM/RAM モード選択
				if value == 1 || value == 0 {
					cpu.bankMode = uint(value)
				}
			case "MBC3":
				if value == 1 {
					// fmt.Println("Latch")
				}
			}
		}
	} else {
		// RAM領域
		if addr >= 0x8000 && addr < 0xa000 {
			// VRAM
			cpu.GPU.VRAMBank[cpu.GPU.VRAMBankPtr][addr-0x8000] = value
		} else if addr >= 0xa000 && addr < 0xc000 {
			if cpu.RTC.Mapped == 0 {
				// RAM
				cpu.RAMBank[cpu.RAMBankPtr][addr-0xa000] = value
			} else {
				cpu.RTC.Write(byte(cpu.RTC.Mapped), value)
			}
		} else if cpu.WRAMBankPtr > 1 && addr >= 0xd000 && addr < 0xe000 {
			// WRAM
			cpu.WRAMBank[cpu.WRAMBankPtr][addr-0xd000] = value
		} else {
			cpu.RAM[addr] = value
		}

		// DMA転送
		if addr == DMAIO {
			start := uint16(cpu.getAReg()) << 8
			for i := 0; i <= 0x9f; i++ {
				cpu.SetMemory8(0xfe00+uint16(i), cpu.FetchMemory8(start+uint16(i)))
			}
			cpu.cycleLine += 150
		}

		// サウンドアクセス
		if addr >= 0xff10 && addr <= 0xff26 {
			cpu.Sound.Write(addr, value)
		}
		if addr >= 0xff30 && addr <= 0xff3f {
			cpu.Sound.WriteWaveform(addr, value)
		}

		if addr == LCDCIO {
			cpu.GPU.LCDC = value
		}
		if addr == LCDSTATIO {
			cpu.GPU.LCDSTAT = value
		}

		if addr == BGPIO {
			cpu.GPU.DMGPallte[0] = value
		} else if addr == OBP0IO {
			cpu.GPU.DMGPallte[1] = value
		} else if addr == OBP1IO {
			cpu.GPU.DMGPallte[2] = value
		}

		if cpu.Cartridge.IsCGB {
			// VRAMバンク切り替え
			if addr == VBKIO {
				newVRAMBankPtr := value & 0x01
				cpu.GPU.VRAMBankPtr = newVRAMBankPtr
			}

			// VRAM DMA転送
			if addr == HDMA5IO {
				fromUpper := uint16(cpu.FetchMemory8(HDMA1IO))
				fromLower := uint16(cpu.FetchMemory8(HDMA2IO) & 0xf0)
				from := (fromUpper << 8) | fromLower
				toUpper := uint16((cpu.FetchMemory8(HDMA3IO) | (1 << 7)) & 0x9f) // 上位3bitは100固定
				toLower := uint16(cpu.FetchMemory8(HDMA4IO) & 0xf0)
				to := (toUpper << 8) | toLower

				HDMA5 := cpu.FetchMemory8(HDMA5IO)
				length := (int((HDMA5 & 0x7f)) + 1) * 16 // 転送するデータ長
				mode := HDMA5 >> 7                       // 転送モード

				switch mode {
				case 0:
					// 汎用DMA
					for i := 0; i < length; i++ {
						value := cpu.FetchMemory8(from + uint16(i))
						cpu.SetMemory8(to+uint16(i), value)
					}
				case 1:
					// H-Blank DMA
					for i := 0; i < length; i++ {
						value := cpu.FetchMemory8(from + uint16(i))
						cpu.SetMemory8(to+uint16(i), value)
					}
					cpu.RAM[HDMA5] = 0xff // 完了
				}
			}

			if addr == BCPSIO {
				cpu.GPU.CGBPallte[0] = value
			} else if addr == OCPSIO {
				cpu.GPU.CGBPallte[1] = value
			} else if addr == BCPDIO {
				// 背景パレットデータ書き込み
				index := cpu.GPU.FetchBGPalleteIndex()
				cpu.GPU.BGPallete[index] = value
				if cpu.GPU.FetchBGPalleteIncrement() {
					cpu.GPU.CGBPallte[0]++
				}
			} else if addr == OCPDIO {
				// スプライトパレットデータ書き込み
				index := cpu.GPU.FetchSPRPalleteIndex()
				cpu.GPU.SPRPallete[index] = value
				if cpu.GPU.FetchSPRPalleteIncrement() {
					cpu.GPU.CGBPallte[1]++
				}
			}

			// WRAMバンク切り替え
			if addr == SVBKIO {
				newWRAMBankPtr := value & 0x07
				if newWRAMBankPtr == 0 {
					newWRAMBankPtr = 1
				}
				cpu.WRAMBankPtr = newWRAMBankPtr
			}
		}
	}
}

// ROMバンクの切り替え
func (cpu *CPU) switchROMBank(newROMBankPtr uint8) {
	switchFlag := false

	switch cpu.Cartridge.ROMSize {
	case 0:
	case 1:
		switchFlag = (newROMBankPtr < 4)
	case 2:
		switchFlag = (newROMBankPtr < 8)
	case 3:
		switchFlag = (newROMBankPtr < 16)
	case 4:
		switchFlag = (newROMBankPtr < 32)
	case 5:
		switchFlag = (newROMBankPtr < 64)
	case 6:
		switchFlag = (newROMBankPtr < 128)
	default:
		errorMsg := fmt.Sprintf("ROMSize is invalid => type:%x rom:%x ram:%x\n", cpu.Cartridge.Type, cpu.Cartridge.ROMSize, cpu.Cartridge.RAMSize)
		panic(errorMsg)
	}

	if switchFlag {
		cpu.ROMBankPtr = newROMBankPtr
	}
}