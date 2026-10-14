using System;
using System.IO;
using System.Collections.Generic;
public class CDriveCleanupTarget {
 public string Root; public string Path; public long Bytes; public long Files;
}
public static class CDriveCleanupSafety {
 public static CDriveCleanupTarget Inspect(string root,string path) { return Scan(root,path,true); }
 public static CDriveCleanupTarget Snapshot(string root,string path) { return Scan(root,path,false); }
 private static CDriveCleanupTarget Scan(string root,string path,bool rejectSourceOrCredentials) {
  root=System.IO.Path.GetFullPath(root).TrimEnd(System.IO.Path.DirectorySeparatorChar);
  path=System.IO.Path.GetFullPath(path);
  if(!path.StartsWith(root+System.IO.Path.DirectorySeparatorChar,StringComparison.OrdinalIgnoreCase))throw new Exception("Outside approved cache root");
  var ancestor=path;
  while(ancestor!=null){
   if((File.GetAttributes(ancestor)&FileAttributes.ReparsePoint)!=0)throw new Exception("Reparse point: "+ancestor);
   ancestor=System.IO.Path.GetDirectoryName(ancestor);
  }
  var result=new CDriveCleanupTarget{Root=root,Path=path}; var pending=new Stack<string>();pending.Push(path);
  while(pending.Count>0){
   var current=pending.Pop();var attrs=File.GetAttributes(current);
   if((attrs&FileAttributes.ReparsePoint)!=0)throw new Exception("Reparse point: "+current);
   var name=System.IO.Path.GetFileName(current);
   if(rejectSourceOrCredentials&&name.Equals(".git",StringComparison.OrdinalIgnoreCase))throw new Exception("Embedded source repository: "+current);
   if((attrs&FileAttributes.Directory)!=0){foreach(var child in Directory.EnumerateFileSystemEntries(current))pending.Push(child);}
   else{
    var ext=System.IO.Path.GetExtension(current).ToLowerInvariant();
    if(rejectSourceOrCredentials&&(name.Equals("gradle.properties",StringComparison.OrdinalIgnoreCase)||name.Equals("key.properties",StringComparison.OrdinalIgnoreCase)||name.StartsWith(".env",StringComparison.OrdinalIgnoreCase)||ext==".keystore"||ext==".jks"||ext==".p12"||ext==".p8"||ext==".pem"))throw new Exception("Potential configuration or credential in target: "+current);
    result.Files++;result.Bytes+=new FileInfo(current).Length;
   }
  }
  return result;
 }
}
