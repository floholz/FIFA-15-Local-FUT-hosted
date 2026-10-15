import ghidra.app.script.GhidraScript;
import ghidra.app.decompiler.*;
import ghidra.program.model.listing.*;
import java.io.*;
public class ExportDecomp extends GhidraScript {
  public void run() throws Exception {
    String out = getScriptArgs()[0];
    DecompInterface di = new DecompInterface(); di.openProgram(currentProgram);
    PrintWriter pw = new PrintWriter(new FileWriter(out));
    FunctionIterator it = currentProgram.getFunctionManager().getFunctions(true);
    int n=0;
    while (it.hasNext() && !monitor.isCancelled()) {
      Function f = it.next();
      DecompileResults r = di.decompileFunction(f, 60, monitor);
      pw.println("//==== FUNC " + f.getEntryPoint() + " " + f.getName() + " body=" + f.getBody().getMinAddress()+"-"+f.getBody().getMaxAddress());
      if (r != null && r.decompileCompleted()) pw.println(r.getDecompiledFunction().getC());
      else pw.println("// decompile failed");
      n++;
    }
    pw.close(); println("exported " + n);
  }
}
