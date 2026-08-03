param(
    [Parameter(Mandatory = $true)]
    [string]$Executable,

    [Parameter(Mandatory = $true)]
    [string]$Icon
)

$ErrorActionPreference = "Stop"
$ExecutablePath = (Resolve-Path $Executable).Path
$IconPath = (Resolve-Path $Icon).Path

$Source = @'
using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.IO;
using System.Runtime.InteropServices;

public static class TautlineIconResource
{
    private const int RT_ICON = 3;
    private const int RT_GROUP_ICON = 14;

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern IntPtr BeginUpdateResource(string fileName, bool deleteExistingResources);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool UpdateResource(
        IntPtr updateHandle,
        IntPtr type,
        IntPtr name,
        ushort language,
        byte[] data,
        uint dataSize);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool EndUpdateResource(IntPtr updateHandle, bool discard);

    private sealed class IconEntry
    {
        public byte Width;
        public byte Height;
        public byte ColorCount;
        public byte Reserved;
        public ushort Planes;
        public ushort BitCount;
        public uint BytesInResource;
        public uint ImageOffset;
        public byte[] ImageData = new byte[0];
    }

    public static void Apply(string executablePath, string iconPath)
    {
        byte[] iconBytes = File.ReadAllBytes(iconPath);
        List<IconEntry> entries = ParseIcon(iconBytes);
        IntPtr handle = BeginUpdateResource(executablePath, false);
        if (handle == IntPtr.Zero)
        {
            throw new Win32Exception(Marshal.GetLastWin32Error(), "BeginUpdateResource failed.");
        }

        bool committed = false;
        try
        {
            for (int index = 0; index < entries.Count; index++)
            {
                IconEntry entry = entries[index];
                int resourceId = index + 1;
                if (!UpdateResource(
                    handle,
                    new IntPtr(RT_ICON),
                    new IntPtr(resourceId),
                    0,
                    entry.ImageData,
                    (uint)entry.ImageData.Length))
                {
                    throw new Win32Exception(Marshal.GetLastWin32Error(), "Updating RT_ICON failed.");
                }
            }

            byte[] groupData = BuildGroupIcon(entries);
            if (!UpdateResource(
                handle,
                new IntPtr(RT_GROUP_ICON),
                new IntPtr(1),
                0,
                groupData,
                (uint)groupData.Length))
            {
                throw new Win32Exception(Marshal.GetLastWin32Error(), "Updating RT_GROUP_ICON failed.");
            }

            if (!EndUpdateResource(handle, false))
            {
                throw new Win32Exception(Marshal.GetLastWin32Error(), "EndUpdateResource failed.");
            }
            committed = true;
        }
        finally
        {
            if (!committed)
            {
                EndUpdateResource(handle, true);
            }
        }
    }

    private static List<IconEntry> ParseIcon(byte[] iconBytes)
    {
        using (MemoryStream stream = new MemoryStream(iconBytes, false))
        using (BinaryReader reader = new BinaryReader(stream))
        {
            ushort reserved = reader.ReadUInt16();
            ushort type = reader.ReadUInt16();
            ushort count = reader.ReadUInt16();
            if (reserved != 0 || type != 1 || count == 0)
            {
                throw new InvalidDataException("The supplied file is not a valid Windows icon.");
            }

            List<IconEntry> entries = new List<IconEntry>(count);
            for (int index = 0; index < count; index++)
            {
                entries.Add(new IconEntry
                {
                    Width = reader.ReadByte(),
                    Height = reader.ReadByte(),
                    ColorCount = reader.ReadByte(),
                    Reserved = reader.ReadByte(),
                    Planes = reader.ReadUInt16(),
                    BitCount = reader.ReadUInt16(),
                    BytesInResource = reader.ReadUInt32(),
                    ImageOffset = reader.ReadUInt32()
                });
            }

            foreach (IconEntry entry in entries)
            {
                stream.Position = entry.ImageOffset;
                entry.ImageData = reader.ReadBytes(checked((int)entry.BytesInResource));
                if (entry.ImageData.Length != entry.BytesInResource)
                {
                    throw new EndOfStreamException("The icon image data is truncated.");
                }
            }

            return entries;
        }
    }

    private static byte[] BuildGroupIcon(List<IconEntry> entries)
    {
        using (MemoryStream stream = new MemoryStream())
        using (BinaryWriter writer = new BinaryWriter(stream))
        {
            writer.Write((ushort)0);
            writer.Write((ushort)1);
            writer.Write((ushort)entries.Count);

            for (int index = 0; index < entries.Count; index++)
            {
                IconEntry entry = entries[index];
                writer.Write(entry.Width);
                writer.Write(entry.Height);
                writer.Write(entry.ColorCount);
                writer.Write(entry.Reserved);
                writer.Write(entry.Planes);
                writer.Write(entry.BitCount);
                writer.Write(entry.BytesInResource);
                writer.Write((ushort)(index + 1));
            }

            writer.Flush();
            return stream.ToArray();
        }
    }
}
'@

if (-not ("TautlineIconResource" -as [type])) {
    Add-Type -TypeDefinition $Source -Language CSharp
}

[TautlineIconResource]::Apply($ExecutablePath, $IconPath)
Write-Host "Embedded icon into $ExecutablePath" -ForegroundColor Green
