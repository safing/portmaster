//nolint:nakedret,unconvert,gocognit,wastedassign,gofumpt
package dsd

func (d *SimpleTestStruct) Size() (s uint64) {

	{
		l := uint64(len(d.S))

		{

			t := l
			for t >= 0x80 {
				t >>= 7
				s++
			}
			s++

		}
		s += l
	}
	s++
	return
}

func (d *SimpleTestStruct) GenCodeMarshal(buf []byte) ([]byte, error) {
	size := d.Size()
	{
		if uint64(cap(buf)) >= size {
			buf = buf[:size]
		} else {
			buf = make([]byte, size)
		}
	}
	i := uint64(0)

	{
		l := uint64(len(d.S))

		{

			t := uint64(l)

			for t >= 0x80 {
				buf[i+0] = byte(t) | 0x80
				t >>= 7
				i++
			}
			buf[i+0] = byte(t)
			i++

		}
		copy(buf[i+0:], d.S)
		i += l
	}
	{
		buf[i+0] = d.B
	}
	return buf[:i+1], nil
}

func (d *SimpleTestStruct) GenCodeUnmarshal(buf []byte) (uint64, error) {
	i := uint64(0)

	{
		l := uint64(0)

		{

			bs := uint8(7)
			t := uint64(buf[i+0] & 0x7F)
			for buf[i+0]&0x80 == 0x80 {
				i++
				t |= uint64(buf[i+0]&0x7F) << bs
				bs += 7
			}
			i++

			l = t

		}
		d.S = string(buf[i+0 : i+0+l])
		i += l
	}
	{
		d.B = buf[i+0]
	}
	return i + 1, nil
}

func (d *GenCodeTestStruct) Size() (s uint64) {

	{
		l := uint64(len(d.S))

		{

			t := l
			for t >= 0x80 {
				t >>= 7
				s++
			}
			s++

		}
		s += l
	}
	{
		if d.Sp != nil {

			{
				l := uint64(len((*d.Sp)))

				{

					t := l
					for t >= 0x80 {
						t >>= 7
						s++
					}
					s++

				}
				s += l
			}
			s += 0
		}
	}
	{
		l := uint64(len(d.Sa))

		{

			t := l
			for t >= 0x80 {
				t >>= 7
				s++
			}
			s++

		}

		for k0 := range d.Sa {

			{
				l := uint64(len(d.Sa[k0]))

				{

					t := l
					for t >= 0x80 {
						t >>= 7
						s++
					}
					s++

				}
				s += l
			}

		}

	}
	{
		if d.Sap != nil {

			{
				l := uint64(len((*d.Sap)))

				{

					t := l
					for t >= 0x80 {
						t >>= 7
						s++
					}
					s++

				}

				for k0 := range *d.Sap {

					{
						l := uint64(len((*d.Sap)[k0]))

						{

							t := l
							for t >= 0x80 {
								t >>= 7
								s++
							}
							s++

						}
						s += l
					}

				}

			}
			s += 0
		}
	}
	{
		if d.Bp != nil {

			s++
		}
	}
	{
		l := uint64(len(d.Ba))

		{

			t := l
			for t >= 0x80 {
				t >>= 7
				s++
			}
			s++

		}
		s += l
	}
	{
		if d.Bap != nil {

			{
				l := uint64(len((*d.Bap)))

				{

					t := l
					for t >= 0x80 {
						t >>= 7
						s++
					}
					s++

				}
				s += l
			}
			s += 0
		}
	}
	s += 35
	return
}

func (d *GenCodeTestStruct) GenCodeMarshal(buf []byte) ([]byte, error) { //nolint:maintidx
	size := d.Size()
	{
		if uint64(cap(buf)) >= size {
			buf = buf[:size]
		} else {
			buf = make([]byte, size)
		}
	}
	i := uint64(0)

	{

		buf[0+0] = byte(d.I8 >> 0)

	}
	{

		buf[0+1] = byte(d.I16 >> 0)

		buf[1+1] = byte(d.I16 >> 8)

	}
	{

		buf[0+3] = byte(d.I32 >> 0)

		buf[1+3] = byte(d.I32 >> 8)

		buf[2+3] = byte(d.I32 >> 16)

		buf[3+3] = byte(d.I32 >> 24)

	}
	{

		buf[0+7] = byte(d.I64 >> 0)

		buf[1+7] = byte(d.I64 >> 8)

		buf[2+7] = byte(d.I64 >> 16)

		buf[3+7] = byte(d.I64 >> 24)

		buf[4+7] = byte(d.I64 >> 32)

		buf[5+7] = byte(d.I64 >> 40)

		buf[6+7] = byte(d.I64 >> 48)

		buf[7+7] = byte(d.I64 >> 56)

	}
	{

		buf[0+15] = byte(d.UI8 >> 0)

	}
	{

		buf[0+16] = byte(d.UI16 >> 0)

		buf[1+16] = byte(d.UI16 >> 8)

	}
	{

		buf[0+18] = byte(d.UI32 >> 0)

		buf[1+18] = byte(d.UI32 >> 8)

		buf[2+18] = byte(d.UI32 >> 16)

		buf[3+18] = byte(d.UI32 >> 24)

	}
	{

		buf[0+22] = byte(d.UI64 >> 0)

		buf[1+22] = byte(d.UI64 >> 8)

		buf[2+22] = byte(d.UI64 >> 16)

		buf[3+22] = byte(d.UI64 >> 24)

		buf[4+22] = byte(d.UI64 >> 32)

		buf[5+22] = byte(d.UI64 >> 40)

		buf[6+22] = byte(d.UI64 >> 48)

		buf[7+22] = byte(d.UI64 >> 56)

	}
	{
		l := uint64(len(d.S))

		{

			t := uint64(l)

			for t >= 0x80 {
				buf[i+30] = byte(t) | 0x80
				t >>= 7
				i++
			}
			buf[i+30] = byte(t)
			i++

		}
		copy(buf[i+30:], d.S)
		i += l
	}
	{
		if d.Sp == nil {
			buf[i+30] = 0
		} else {
			buf[i+30] = 1

			{
				l := uint64(len((*d.Sp)))

				{

					t := uint64(l)

					for t >= 0x80 {
						buf[i+31] = byte(t) | 0x80
						t >>= 7
						i++
					}
					buf[i+31] = byte(t)
					i++

				}
				copy(buf[i+31:], (*d.Sp))
				i += l
			}
			i += 0
		}
	}
	{
		l := uint64(len(d.Sa))

		{

			t := uint64(l)

			for t >= 0x80 {
				buf[i+31] = byte(t) | 0x80
				t >>= 7
				i++
			}
			buf[i+31] = byte(t)
			i++

		}
		for k0 := range d.Sa {

			{
				l := uint64(len(d.Sa[k0]))

				{

					t := uint64(l)

					for t >= 0x80 {
						buf[i+31] = byte(t) | 0x80
						t >>= 7
						i++
					}
					buf[i+31] = byte(t)
					i++

				}
				copy(buf[i+31:], d.Sa[k0])
				i += l
			}

		}
	}
	{
		if d.Sap == nil {
			buf[i+31] = 0
		} else {
			buf[i+31] = 1

			{
				l := uint64(len((*d.Sap)))

				{

					t := uint64(l)

					for t >= 0x80 {
						buf[i+32] = byte(t) | 0x80
						t >>= 7
						i++
					}
					buf[i+32] = byte(t)
					i++

				}
				for k0 := range *d.Sap {

					{
						l := uint64(len((*d.Sap)[k0]))

						{

							t := uint64(l)

							for t >= 0x80 {
								buf[i+32] = byte(t) | 0x80
								t >>= 7
								i++
							}
							buf[i+32] = byte(t)
							i++

						}
						copy(buf[i+32:], (*d.Sap)[k0])
						i += l
					}

				}
			}
			i += 0
		}
	}
	{
		buf[i+32] = d.B
	}
	{
		if d.Bp == nil {
			buf[i+33] = 0
		} else {
			buf[i+33] = 1

			{
				buf[i+34] = (*d.Bp)
			}
			i++
		}
	}
	{
		l := uint64(len(d.Ba))

		{

			t := uint64(l)

			for t >= 0x80 {
				buf[i+34] = byte(t) | 0x80
				t >>= 7
				i++
			}
			buf[i+34] = byte(t)
			i++

		}
		copy(buf[i+34:], d.Ba)
		i += l
	}
	{
		if d.Bap == nil {
			buf[i+34] = 0
		} else {
			buf[i+34] = 1

			{
				l := uint64(len((*d.Bap)))

				{

					t := uint64(l)

					for t >= 0x80 {
						buf[i+35] = byte(t) | 0x80
						t >>= 7
						i++
					}
					buf[i+35] = byte(t)
					i++

				}
				copy(buf[i+35:], (*d.Bap))
				i += l
			}
			i += 0
		}
	}
	return buf[:i+35], nil
}

func (d *GenCodeTestStruct) GenCodeUnmarshal(buf []byte) (uint64, error) { //nolint:maintidx
	i := uint64(0)

	{

		d.I8 = 0 | (int8(buf[i+0+0]) << 0)

	}
	{

		d.I16 = 0 | (int16(buf[i+0+1]) << 0) | (int16(buf[i+1+1]) << 8)

	}
	{

		d.I32 = 0 | (int32(buf[i+0+3]) << 0) | (int32(buf[i+1+3]) << 8) | (int32(buf[i+2+3]) << 16) | (int32(buf[i+3+3]) << 24)

	}
	{

		d.I64 = 0 | (int64(buf[i+0+7]) << 0) | (int64(buf[i+1+7]) << 8) | (int64(buf[i+2+7]) << 16) | (int64(buf[i+3+7]) << 24) | (int64(buf[i+4+7]) << 32) | (int64(buf[i+5+7]) << 40) | (int64(buf[i+6+7]) << 48) | (int64(buf[i+7+7]) << 56)

	}
	{

		d.UI8 = 0 | (uint8(buf[i+0+15]) << 0)

	}
	{

		d.UI16 = 0 | (uint16(buf[i+0+16]) << 0) | (uint16(buf[i+1+16]) << 8)

	}
	{

		d.UI32 = 0 | (uint32(buf[i+0+18]) << 0) | (uint32(buf[i+1+18]) << 8) | (uint32(buf[i+2+18]) << 16) | (uint32(buf[i+3+18]) << 24)

	}
	{

		d.UI64 = 0 | (uint64(buf[i+0+22]) << 0) | (uint64(buf[i+1+22]) << 8) | (uint64(buf[i+2+22]) << 16) | (uint64(buf[i+3+22]) << 24) | (uint64(buf[i+4+22]) << 32) | (uint64(buf[i+5+22]) << 40) | (uint64(buf[i+6+22]) << 48) | (uint64(buf[i+7+22]) << 56)

	}
	{
		l := uint64(0)

		{

			bs := uint8(7)
			t := uint64(buf[i+30] & 0x7F)
			for buf[i+30]&0x80 == 0x80 {
				i++
				t |= uint64(buf[i+30]&0x7F) << bs
				bs += 7
			}
			i++

			l = t

		}
		d.S = string(buf[i+30 : i+30+l])
		i += l
	}
	{
		if buf[i+30] == 1 {
			if d.Sp == nil {
				d.Sp = new(string)
			}

			{
				l := uint64(0)

				{

					bs := uint8(7)
					t := uint64(buf[i+31] & 0x7F)
					for buf[i+31]&0x80 == 0x80 {
						i++
						t |= uint64(buf[i+31]&0x7F) << bs
						bs += 7
					}
					i++

					l = t

				}
				(*d.Sp) = string(buf[i+31 : i+31+l])
				i += l
			}
			i += 0
		} else {
			d.Sp = nil
		}
	}
	{
		l := uint64(0)

		{

			bs := uint8(7)
			t := uint64(buf[i+31] & 0x7F)
			for buf[i+31]&0x80 == 0x80 {
				i++
				t |= uint64(buf[i+31]&0x7F) << bs
				bs += 7
			}
			i++

			l = t

		}
		if uint64(cap(d.Sa)) >= l {
			d.Sa = d.Sa[:l]
		} else {
			d.Sa = make([]string, l)
		}
		for k0 := range d.Sa {

			{
				l := uint64(0)

				{

					bs := uint8(7)
					t := uint64(buf[i+31] & 0x7F)
					for buf[i+31]&0x80 == 0x80 {
						i++
						t |= uint64(buf[i+31]&0x7F) << bs
						bs += 7
					}
					i++

					l = t

				}
				d.Sa[k0] = string(buf[i+31 : i+31+l])
				i += l
			}

		}
	}
	{
		if buf[i+31] == 1 {
			if d.Sap == nil {
				d.Sap = new([]string)
			}

			{
				l := uint64(0)

				{

					bs := uint8(7)
					t := uint64(buf[i+32] & 0x7F)
					for buf[i+32]&0x80 == 0x80 {
						i++
						t |= uint64(buf[i+32]&0x7F) << bs
						bs += 7
					}
					i++

					l = t

				}
				if uint64(cap((*d.Sap))) >= l {
					(*d.Sap) = (*d.Sap)[:l]
				} else {
					(*d.Sap) = make([]string, l)
				}
				for k0 := range *d.Sap {

					{
						l := uint64(0)

						{

							bs := uint8(7)
							t := uint64(buf[i+32] & 0x7F)
							for buf[i+32]&0x80 == 0x80 {
								i++
								t |= uint64(buf[i+32]&0x7F) << bs
								bs += 7
							}
							i++

							l = t

						}
						(*d.Sap)[k0] = string(buf[i+32 : i+32+l])
						i += l
					}

				}
			}
			i += 0
		} else {
			d.Sap = nil
		}
	}
	{
		d.B = buf[i+32]
	}
	{
		if buf[i+33] == 1 {
			if d.Bp == nil {
				d.Bp = new(byte)
			}

			{
				(*d.Bp) = buf[i+34]
			}
			i++
		} else {
			d.Bp = nil
		}
	}
	{
		l := uint64(0)

		{

			bs := uint8(7)
			t := uint64(buf[i+34] & 0x7F)
			for buf[i+34]&0x80 == 0x80 {
				i++
				t |= uint64(buf[i+34]&0x7F) << bs
				bs += 7
			}
			i++

			l = t

		}
		if uint64(cap(d.Ba)) >= l {
			d.Ba = d.Ba[:l]
		} else {
			d.Ba = make([]byte, l)
		}
		copy(d.Ba, buf[i+34:])
		i += l
	}
	{
		if buf[i+34] == 1 {
			if d.Bap == nil {
				d.Bap = new([]byte)
			}

			{
				l := uint64(0)

				{

					bs := uint8(7)
					t := uint64(buf[i+35] & 0x7F)
					for buf[i+35]&0x80 == 0x80 {
						i++
						t |= uint64(buf[i+35]&0x7F) << bs
						bs += 7
					}
					i++

					l = t

				}
				if uint64(cap((*d.Bap))) >= l {
					(*d.Bap) = (*d.Bap)[:l]
				} else {
					(*d.Bap) = make([]byte, l)
				}
				copy((*d.Bap), buf[i+35:])
				i += l
			}
			i += 0
		} else {
			d.Bap = nil
		}
	}
	return i + 35, nil
}
