using System;
using System.Runtime.InteropServices;
using Mono.Unix;
using Mono.Unix.Native;

namespace XStream {

	public class XStreamFD
	{
		public XStreamFD (int fileDescriptor, bool ownsHandle)
		{
			this.fileDescriptor = fileDescriptor;
			this.owner = ownsHandle;
		}

		public int Handle
		{
			get { return fileDescriptor; }
		}

		public unsafe int Read ([In, Out] byte[] buffer, int offset, int count)
		{
			long r = 0;
			fixed (byte* buf = &buffer[offset]) {
				do {
					r = Syscall.read (fileDescriptor, buf, (ulong) count);
				} while (UnixMarshal.ShouldRetrySyscall ((int) r));
			}
			if (r == -1)
				UnixMarshal.ThrowExceptionForLastError ();
			return (int) r;
		}

		public unsafe void Write (byte[] buffer, int offset, int count)
		{
			long r = 0;
			fixed (byte* buf = &buffer[offset]) {
				do {
					r = Syscall.write (fileDescriptor, buf, (ulong) count);
				} while (UnixMarshal.ShouldRetrySyscall ((int) r));
			}
			if (r == -1)
				UnixMarshal.ThrowExceptionForLastError ();
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
				r = Syscall.close (fileDescriptor);
			} while (UnixMarshal.ShouldRetrySyscall (r));
			UnixMarshal.ThrowExceptionForLastErrorIf (r);
			fileDescriptor = -1;
			owner = false;
			GC.SuppressFinalize (this);
		}
		
		private bool owner = true;
		private int fileDescriptor = -1;
	}
}
