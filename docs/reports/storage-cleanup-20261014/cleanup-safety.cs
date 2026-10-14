using System;
using System.IO;
using System.Collections.Generic;
public class BirdtieTreeInfo { public long Bytes; public long Files; }
public static class BirdtieCleanupSafety {
 public static BirdtieTreeInfo Inspect(string root, string path, HashSet<string> protectedFiles) {
  root=Path.GetFullPath(root).TrimEnd(Path.DirectorySeparatorChar);
  path=Path.GetFullPath(path);
  if(!path.StartsWith(root+Path.DirectorySeparatorChar,StringComparison.OrdinalIgnoreCase))throw new Exception("Outside intended root: "+path);
  var ancestor=path;
  while(ancestor!=null && ancestor.Length>=root.Length){
   if((File.GetAttributes(ancestor)&FileAttributes.ReparsePoint)!=0)throw new Exception("Reparse point: "+ancestor);
   if(ancestor.Equals(root,StringComparison.OrdinalIgnoreCase))break;
   ancestor=Path.GetDirectoryName(ancestor);
  }
  var result=new BirdtieTreeInfo();var stack=new Stack<string>();stack.Push(path);
  while(stack.Count>0){
   var current=stack.Pop();var attrs=File.GetAttributes(current);
   if((attrs&FileAttributes.ReparsePoint)!=0)throw new Exception("Reparse point: "+current);
   if((attrs&FileAttributes.Directory)!=0){foreach(var entry in Directory.EnumerateFileSystemEntries(current))stack.Push(entry);}
   else{var rel=current.Substring(root.Length+1).Replace('\\','/');if(protectedFiles.Contains(rel))throw new Exception("Protected Git/source file: "+rel);result.Bytes+=new FileInfo(current).Length;result.Files++;}
  }
  return result;
 }
}
