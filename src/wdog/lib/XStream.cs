// Inspired by Mono.Posix UnixStream
using System;
using System.Runtime.InteropServices;
using Mono.Unix;
using Mono.Unix.Native;

namespace XStream {

	public class XStreamFD
	{
		public XStreamFD (int fd, bool ownsHandle)
		{
			this.fd = fd;
			this.owner = ownsHandle;
		}

		public int Handle
		{
			get { return fd; }
		}

		public unsafe int Read ([In, Out] byte[] buffer, int offset, int count)
		{
			long r = 0;
			fixed (byte* buf = &buffer[offset]) {
				do {
					r = Syscall.read (fd, buf, (ulong) count);
				} while (UnixMarshal.ShouldRetrySyscall ((int) r));
			}
			return (int) r;
		}

		public unsafe int Write (byte[] buffer, int offset, int count)
		{
			long r = 0;
			fixed (byte* buf = &buffer[offset]) {
				do {
					r = Syscall.write (fd, buf, (ulong) count);
				} while (UnixMarshal.ShouldRetrySyscall ((int) r));
			}
			return (int) r;
		}
		
		~XStreamFD ()
		{
			Close ();
		}

		public void Close ()
		{
			if (!owner)
				return;

			int r;
			do {
				r = Syscall.close (fd);
			} while (UnixMarshal.ShouldRetrySyscall (r));
			fd = -1;
			owner = false;
			GC.SuppressFinalize (this);
		}
		
		private bool owner = true;
		private int fd = -1;
	}
}
